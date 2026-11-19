package discovery

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/catalog"
	data "github.com/lober/lober/internal/domain/discovery"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

const attemptLife = 15 * 60

type Image struct {
	Kernel string `yaml:"kernel"`
	Initrd string `yaml:"initrd"`
}

func (i Image) Valid() bool {
	return artifactPath(i.Kernel) && artifactPath(i.Initrd)
}

func artifactPath(s string) bool {
	for _, part := range strings.Split(s, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return !strings.ContainsFunc(s, func(r rune) bool {
		return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || strings.ContainsRune("._-/", r))
	})
}

// Console 是管理员查看与确认发现记录的入口。
type Console interface {
	List(context.Context, ports.Actor, string, string, int) (Page, error)
	Get(context.Context, ports.Actor, string) (data.Record, error)
	Register(context.Context, ports.Actor, string, string) (model.Host, error)
}

// Machine 是引导协议和采集上报的入口。
type Machine interface {
	Boot(ctx context.Context, sn, mac string, arch catalog.Arch, firmware catalog.Firmware) ([]byte, error)
	RegisteredSerial(ctx context.Context, mac, sn string) (string, error)
	Submit(ctx context.Context, mac, token string, body []byte) error
}

type Store interface {
	repository.Discoveries
	GetHost(context.Context, string) (model.Host, error)
}

type Tokens interface{ NewToken() (string, error) }

type Service struct {
	tokens             Tokens
	enabled            bool
	store              Store
	clock              ports.Clock
	key                []byte
	gateway, artifacts string
	images             map[catalog.Arch]Image
}

func New(store Store, clock ports.Clock, key []byte, gateway, artifacts string, images map[catalog.Arch]Image, enabled bool, tokens Tokens) *Service {
	return &Service{tokens: tokens, enabled: enabled, store: store, clock: clock, key: append([]byte(nil), key...), gateway: strings.TrimRight(gateway, "/"), artifacts: artifacts, images: maps.Clone(images)}
}

func (s *Service) RegisteredSerial(ctx context.Context, rawMAC, rawSN string) (string, error) {
	mac := data.MAC(rawMAC)
	if mac == "" {
		return "", nil
	}
	observed, registered, err := s.store.RegisteredDiscoverySN(ctx, mac)
	if err != nil || registered == "" {
		return "", err
	}
	sn, _ := model.NormalizeSN(rawSN)
	// 管理员可更正出厂串号。只认可登记时的原串号或更正后的串号。
	if sn != registered && sn != observed {
		return "", data.ErrIdentityConflict
	}
	return registered, nil
}

func (s *Service) Boot(ctx context.Context, rawSN, rawMAC string, arch catalog.Arch, fw catalog.Firmware) ([]byte, error) {
	if !s.enabled {
		return nil, nil
	}
	mac := data.MAC(rawMAC)
	if mac == "" || !arch.Valid() || !fw.Valid() {
		return nil, nil
	}
	sn, p := model.NormalizeSN(rawSN)
	if p != nil {
		sn = ""
	}
	if sn != "" {
		_, err := s.store.GetHost(ctx, sn)
		if err == nil {
			return nil, nil
		}
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, err
		}
	}
	at := s.clock()
	attempt := data.Attempt{MAC: mac, SN: sn, Arch: arch, Firmware: fw, At: at}
	image, ok := s.images[arch]
	if !ok {
		attempt.Reason = "未配置该架构的采集镜像"
	} else {
		nonce, err := s.tokens.NewToken()
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256([]byte(nonce))
		attempt.ID = hex.EncodeToString(sum[:16])
		attempt.ExpiresAt = at + attemptLife
	}
	attempt, err := s.store.ObserveDiscovery(ctx, attempt)
	if err != nil || attempt.ID == "" {
		return nil, err
	}
	serialConsole := "ttyS0"
	if arch == catalog.Arch("aarch64") {
		serialConsole = "ttyAMA0"
	}
	return fmt.Appendf(nil, "#!ipxe\nkernel %s initrd=discovery.img quiet loglevel=4 lober.discovery_url=%s/v1/discovery/%s lober.discovery_token=%s lober.discovery_mac=%s console=tty0 console=%s,115200n8 || exit\ninitrd --name discovery.img %s || exit\nboot || exit\n",
		catalog.ArtifactURL(s.artifacts, image.Kernel), s.gateway, mac, s.token(mac, attempt.ID), mac, serialConsole, catalog.ArtifactURL(s.artifacts, image.Initrd)), nil
}

func (s *Service) token(mac, id string) string {
	m := hmac.New(sha256.New, s.key)
	fmt.Fprintf(m, "discovery:%s:%s", mac, id)
	return id + hex.EncodeToString(m.Sum(nil))
}

func (s *Service) Submit(ctx context.Context, rawMAC, token string, body []byte) error {
	if !s.enabled {
		return data.ErrDisabled
	}
	mac := data.MAC(rawMAC)
	if mac == "" || len(token) != 96 || !hmac.Equal([]byte(token), []byte(s.token(mac, token[:32]))) {
		return data.ErrToken
	}
	report, err := data.ParseReport(body)
	if err != nil {
		return err
	}
	return s.store.SaveDiscoveryReport(ctx, mac, token[:32], s.clock(), report)
}

type Page struct {
	Items      []data.Record `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func (s *Service) List(ctx context.Context, a ports.Actor, cursor, registeredSN string, limit int) (Page, error) {
	if err := a.Authorize(access.PermHostRead); err != nil {
		return Page{}, err
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 || (cursor != "" && data.MAC(cursor) != cursor) {
		return Page{}, repository.ErrInvalid
	}
	if registeredSN != "" {
		normalized, err := model.NormalizeSN(registeredSN)
		if err != nil || cursor != "" {
			return Page{}, repository.ErrInvalid
		}
		registeredSN = normalized
	}
	records, err := s.store.ListDiscoveries(ctx, cursor, registeredSN, limit+1, s.clock())
	if err != nil {
		return Page{}, err
	}
	if records == nil {
		records = []data.Record{}
	}
	page := Page{Items: records}
	if len(records) > limit {
		page.Items = records[:limit]
		page.NextCursor = page.Items[limit-1].MAC
	}
	return page, nil
}

func (s *Service) Get(ctx context.Context, a ports.Actor, rawMAC string) (data.Record, error) {
	if err := a.Authorize(access.PermHostRead); err != nil {
		return data.Record{}, err
	}
	mac := data.MAC(rawMAC)
	if mac == "" {
		return data.Record{}, repository.ErrInvalid
	}
	return s.store.GetDiscovery(ctx, mac, s.clock())
}

func (s *Service) Register(ctx context.Context, a ports.Actor, rawMAC, rawSN string) (model.Host, error) {
	if err := a.Authorize(access.PermHostWrite); err != nil {
		return model.Host{}, err
	}
	if a.Role != model.RoleAdmin {
		return model.Host{}, ports.ErrForbidden
	}
	mac := data.MAC(rawMAC)
	sn, p := model.NormalizeSN(rawSN)
	if mac == "" || p != nil {
		return model.Host{}, repository.ErrInvalid
	}
	return s.store.RegisterDiscovery(ctx, mac, sn, repository.Audit{Actor: a.Username, Action: "host.create", At: s.clock(), Detail: "PXE " + mac})
}
