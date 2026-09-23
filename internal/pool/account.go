package pool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/teddyli18000/mimo-web-proxy/internal/config"
	"github.com/teddyli18000/mimo-web-proxy/internal/mimo"
)

type Pool struct {
	clients  []*entry
	counter  atomic.Uint64
	mu       sync.RWMutex
}

type entry struct {
	account config.Account
	client  *mimo.WebClient
	healthy bool
}

func New(accounts []config.Account) *Pool {
	p := &Pool{}
	for _, acc := range accounts {
		if !acc.Active {
			continue
		}
		p.clients = append(p.clients, &entry{
			account: acc,
			client:  mimo.NewWebClient(acc.ServiceToken, acc.UserID, acc.Ph),
			healthy: true,
		})
	}
	return p
}

// Next 获取下一个可用客户端
func (p *Pool) Next() (*mimo.WebClient, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.clients) == 0 {
		return nil, fmt.Errorf("no accounts configured")
	}
	n := len(p.clients)
	for i := 0; i < n; i++ {
		idx := int(p.counter.Add(1)) % n
		if p.clients[idx].healthy {
			return p.clients[idx].client, nil
		}
	}
	return p.clients[0].client, nil
}

func (p *Pool) HasAccounts() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.clients) > 0
}

func (p *Pool) Reload(accounts []config.Account) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients = nil
	for _, acc := range accounts {
		if !acc.Active {
			continue
		}
		p.clients = append(p.clients, &entry{
			account: acc,
			client:  mimo.NewWebClient(acc.ServiceToken, acc.UserID, acc.Ph),
			healthy: true,
		})
	}
}

func (p *Pool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.clients)
}

func (p *Pool) HealthCheck(ctx context.Context) map[string]bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	results := make(map[string]bool)
	for _, e := range p.clients {
		err := e.client.Validate(ctx)
		e.healthy = err == nil
		results[e.account.ID] = e.healthy
	}
	return results
}
