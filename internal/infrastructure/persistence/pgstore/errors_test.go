package pgstore

import (
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lober/lober/internal/domain/repository"
	"testing"
)

func TestRestrictViolation(t *testing.T) {
	for _, code := range []string{"23503", "23001"} {
		e := &pgconn.PgError{Code: code, ConstraintName: "fk_tasks_iso"}
		if got := translate(e, true, nil); !errors.Is(got, repository.ErrReferenced) {
			t.Fatalf("%s: %v", code, got)
		}
	}
	e := &pgconn.PgError{Code: "23001", ConstraintName: "unknown"}
	if translate(e, true, nil) != e || translate(e, false, nil) != e {
		t.Fatal("unrelated restrictions must remain unchanged")
	}
}
