package webapp

import (
	"context"
	"net/http"

	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
)

// Analysis and coordinator chat share the same bounded local search adapter.
type searxNGSearch struct {
	baseURL string
	client  *http.Client
}

func (s searxNGSearch) SearchWeb(ctx context.Context, query string) ([]analysisWebHit, error) {
	return intake.SearchWeb(ctx, s.baseURL, query, s.client)
}
