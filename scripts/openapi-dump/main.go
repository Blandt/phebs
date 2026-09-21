// Command openapi-dump builds the phebs huma API in-process (with a no-op
// store, mirroring internal/api tests) and prints the OpenAPI document that
// the server serves at /api/openapi.json. Used by `npm run spec:dump` in ui/.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/bmeddeb/phebs/internal/api"
	"github.com/bmeddeb/phebs/internal/servicecatalog"
	"github.com/bmeddeb/phebs/internal/store"
)

// noopStore embeds the store interface so unimplemented methods panic only if
// actually called; OpenAPI generation never touches the store.
type noopStore struct{ store.Store }

// The service directory gates route registration on a small store capability.
// Stub it so the dump covers /api/services and /api/service.
func (noopStore) GetServiceStateRead(context.Context, string, string) (*store.ServiceStateRead, error) {
	return nil, nil
}

func (noopStore) ConfirmServiceStateSnapshot(
	context.Context, string, servicecatalog.RepositoryState,
) error {
	return nil
}

func (noopStore) ListServiceStates(
	context.Context, string, store.ServiceStateFilter, store.ServiceStatePosition, int,
) (*store.ServiceStatePage, error) {
	return nil, nil
}

func main() {
	h := api.New(api.Options{
		Version: "openapi-dump",
		Store:   &noopStore{},
		// Zero principal/fixture unlock the contract catalog routes without
		// a real store or evidence backend.
		Principal:              func(context.Context) string { return "openapi-dump" },
		ContractCatalogFixture: &api.ContractCatalogFixture{},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		fmt.Fprintf(os.Stderr, "openapi.json status = %d\n", rec.Code)
		os.Exit(1)
	}
	if _, err := io.Copy(os.Stdout, rec.Result().Body); err != nil {
		fmt.Fprintf(os.Stderr, "copy: %v\n", err)
		os.Exit(1)
	}
}
