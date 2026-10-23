package media

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func Test来源错误转换保留其他原因(t *testing.T) {
	cause := errors.New("restore failed")
	other := errors.New("other failure")
	link := &os.LinkError{Op: "rename", Old: "backup", New: "part", Err: cause}
	wrapped := fmt.Errorf("restore files: %w", link)
	fault := &port.Fault{Kind: port.InvalidFile}
	for name, input := range map[string]error{
		"并列":   errors.Join(fault, wrapped),
		"嵌套":   fmt.Errorf("operation: %w", errors.Join(errors.Join(fault, wrapped), other)),
		"多个故障": errors.Join(fault, &port.Fault{Kind: port.URLRejected}, wrapped),
	} {
		t.Run(name, func(t *testing.T) {
			got := (&Sources{}).problem(model.Source{ISOID: "image", Role: "netboot"}, input)
			var matched *os.LinkError
			if !errors.Is(got, repository.ErrInvalid) || !errors.Is(got, cause) || !errors.Is(got, wrapped) || !errors.As(got, &matched) || matched != link {
				t.Fatal("转换丢失独立错误分支", got)
			}
			if name == "多个故障" {
				var problems *repository.ProblemsError
				if !errors.As(got, &problems) || len(problems.Problems) != 1 || problems.Problems[0].Subject.Path != "source.netboot" {
					t.Fatal(got)
				}
			}
			if name == "嵌套" && !errors.Is(got, other) {
				t.Fatal("嵌套后的其他原因丢失", got)
			}
		})
	}
	service := &Sources{}
	if service.problem(model.Source{}, wrapped) != wrapped || service.problem(model.Source{}, nil) != nil {
		t.Fatal("普通错误或空错误发生变化")
	}
	unknown := fmt.Errorf("unchanged: %w", &port.Fault{Kind: "future_fault", Cause: cause})
	if service.problem(model.Source{}, unknown) != unknown {
		t.Fatal("未知故障发生变化")
	}
}

func Test来源错误转换继续隐藏单故障细节(t *testing.T) {
	secret := errors.New("private network address")
	for kind, want := range map[port.FaultKind]error{
		port.URLRejected: repository.ErrInvalid, port.PathRejected: repository.ErrInvalid,
		port.ChunkRejected: repository.ErrInvalid, port.ContentMismatch: repository.ErrRejected,
		port.PublicationConflict: repository.ErrConflict, port.InvalidFile: repository.ErrInvalid,
	} {
		t.Run(string(kind), func(t *testing.T) {
			fault := &port.Fault{Kind: kind, Cause: secret, Expected: "expected", Actual: "actual"}
			for _, input := range []error{fault, fmt.Errorf("private wrapper: %w", fault), errors.Join(fault, errors.New("independent failure"))} {
				got := (&Sources{}).problem(model.Source{ISOID: "image"}, input)
				var leaked *port.Fault
				if !errors.Is(got, want) || errors.Is(got, secret) || errors.As(got, &leaked) || strings.Contains(got.Error(), "private") {
					t.Fatal(got)
				}
			}
		})
	}
}
