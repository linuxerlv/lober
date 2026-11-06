package inventory

import (
	"context"
	"errors"
	"strconv"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func (s *Service) Request(ctx context.Context, a ports.Actor, rawSN string) (model.Host, error) {
	if err := a.Authorize(access.PermHostWrite); err != nil {
		return model.Host{}, err
	}
	sn, err := model.NormalizeSN(rawSN)
	if err != nil {
		return model.Host{}, err
	}
	var previous string
	updated, err := s.hosts.UpdateInventory(ctx, sn, func(current model.Host, busy bool) (repository.InventoryChange, error) {
		if busy {
			return repository.InventoryChange{}, ErrHostBusy
		}
		batch, err := s.newBatch()
		if err != nil {
			return repository.InventoryChange{}, err
		}
		at := s.now()
		attrs := cloneAttrs(current.Attrs)
		previous = attrs[AttrRequestID]
		attrs[AttrRequestID] = batch
		attrs[AttrRequested] = strconv.FormatInt(at, 10)
		delete(attrs, AttrInFlight)
		return inventoryChange(attrs, a.Username, "host.inventory_request", "", at), nil
	})
	if errors.Is(err, repository.ErrNotFound) {
		return model.Host{}, ErrUnknownHost
	}
	if err != nil {
		return model.Host{}, err
	}
	s.revoke(sn, previous)
	return updated, nil
}

// ClaimToken 决定本次引导是否进入采集，并为锁内读到的同一批次发令牌。
// 没有待处理请求、正在装机、已超时或无法生成令牌时，返回空串。
func (s *Service) ClaimToken(ctx context.Context, rawSN string) (string, error) {
	sn, err := model.NormalizeSN(rawSN)
	if err != nil {
		return "", nil
	}
	var token, expired string
	_, err = s.hosts.UpdateInventory(ctx, sn, func(current model.Host, busy bool) (repository.InventoryChange, error) {
		attrs := current.Attrs
		batch := attrs[AttrRequestID]
		requested, requestedOK := atoi(attrs[AttrRequested])
		collected, collectedOK := atoi(attrs[AttrCollectedAt])
		if busy || !requestedOK || (batch == "" && collectedOK && requested <= collected) {
			return repository.InventoryChange{}, nil
		}
		at := s.now()
		started, startedOK := atoi(attrs[AttrInFlight])
		if startedOK && at-started >= TokenLife {
			expired = batch
			next := cloneAttrs(attrs)
			clearRequest(next)
			if collectedOK {
				next[AttrRequested] = attrs[AttrCollectedAt]
			}
			return inventoryChange(next, "inventory", "host.inventory_abandon", "", at), nil
		}
		legacy := batch == ""
		if legacy {
			var err error
			batch, err = s.newBatch()
			if err != nil {
				return repository.InventoryChange{}, nil
			}
		}
		if !startedOK {
			started = at
		}
		token = s.issue(sn, batch, started+TokenLife)
		if token == "" || (!legacy && startedOK) {
			return repository.InventoryChange{}, nil
		}
		next := cloneAttrs(attrs)
		next[AttrRequestID] = batch
		next[AttrInFlight] = strconv.FormatInt(started, 10)
		return inventoryChange(next, "inventory", "host.inventory_claim", "", at), nil
	})
	if errors.Is(err, repository.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if expired != "" {
		s.revoke(sn, expired)
	}
	return token, nil
}

func (s *Service) newBatch() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keys.NewToken()
}

// issue 只在持有主机锁时调用，避免迟到的旧批次令牌顶掉新令牌。
func (s *Service) issue(sn, batch string, expires int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g, ok := s.grants[sn]; ok && g.batch == batch && g.expires == expires {
		return g.token
	}
	token, err := s.keys.NewToken()
	if err != nil {
		return ""
	}
	s.grants[sn] = grant{batch: batch, token: token, expires: expires}
	return token
}

func clearRequest(attrs map[string]string) {
	delete(attrs, AttrRequestID)
	delete(attrs, AttrRequested)
	delete(attrs, AttrInFlight)
}

func inventoryChange(attrs map[string]string, actor, action, detail string, at int64) repository.InventoryChange {
	return repository.InventoryChange{Attrs: attrs, Audit: repository.Audit{Actor: actor, Action: action, Detail: detail, At: at}}
}
