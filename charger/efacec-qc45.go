package charger

import (
	"context"
	"fmt"
	"strings"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/util"
)

// EfacecQC45 implements the basic evcc charger interface for the Efacec QC45.
//
// QC45 chargers expose vendor-specific web services over IP and may also be
// connected to a backend using OCPP. The public QC45 documentation does not
// specify the vendor web-service endpoints or payloads. Protocol operations are
// therefore kept explicit here and will be implemented against a verified QC45
// protocol trace/documentation instead of relying on guessed endpoints.
type EfacecQC45 struct {
	uri       string
	connector int
}

func init() {
	registry.AddCtx("efacec-qc45", NewEfacecQC45FromConfig)
}

// NewEfacecQC45FromConfig creates an Efacec QC45 charger from generic config.
func NewEfacecQC45FromConfig(ctx context.Context, other map[string]any) (api.Charger, error) {
	cc := struct {
		URI       string
		Connector int
	}{
		Connector: 1,
	}

	if err := util.DecodeOther(other, &cc); err != nil {
		return nil, err
	}

	return NewEfacecQC45(ctx, cc.URI, cc.Connector)
}

// NewEfacecQC45 creates an Efacec QC45 charger.
func NewEfacecQC45(_ context.Context, uri string, connector int) (*EfacecQC45, error) {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return nil, fmt.Errorf("uri must not be empty")
	}

	if connector < 1 {
		return nil, fmt.Errorf("invalid connector: %d", connector)
	}

	return &EfacecQC45{
		uri:       strings.TrimRight(uri, "/"),
		connector: connector,
	}, nil
}

// Status implements the api.Charger interface.
func (c *EfacecQC45) Status() (api.ChargeStatus, error) {
	return api.StatusNone, fmt.Errorf("efacec QC45 status via %s connector %d: %w", c.uri, c.connector, api.ErrNotAvailable)
}

// Enabled implements the api.Charger interface.
func (c *EfacecQC45) Enabled() (bool, error) {
	return false, fmt.Errorf("efacec QC45 enabled state via %s connector %d: %w", c.uri, c.connector, api.ErrNotAvailable)
}

// Enable implements the api.Charger interface.
func (c *EfacecQC45) Enable(bool) error {
	return fmt.Errorf("efacec QC45 enable command via %s connector %d: %w", c.uri, c.connector, api.ErrNotAvailable)
}

// MaxCurrent implements the api.Charger interface.
func (c *EfacecQC45) MaxCurrent(int64) error {
	return fmt.Errorf("efacec QC45 current limit via %s connector %d: %w", c.uri, c.connector, api.ErrNotAvailable)
}

var _ api.Charger = (*EfacecQC45)(nil)
