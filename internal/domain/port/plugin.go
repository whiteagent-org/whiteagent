// Package port defines all plugin port interfaces for the whiteagent runtime.
// Every plugin kind must implement its respective interface here.
// This package depends only on the entity and dto packages — no external deps.
package port

import (
	"context"
	"encoding/json"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// Plugin is the base interface embedded by all plugin kind interfaces.
// Every plugin must implement ID, Kind, Init, Start, Stop, and Status.
type Plugin interface {
	ID() string
	Kind() entity.PluginKind
	Init(ctx context.Context, id string, cfg json.RawMessage) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Status() entity.PluginState
}

// PluginManifest is returned by the Manifest() symbol exported from a plugin .so file.
// It is read before NewPlugin() is called, enabling pre-instantiation kind validation.
type PluginManifest struct {
	Kind entity.PluginKind
}
