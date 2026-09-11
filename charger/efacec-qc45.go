package charger

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/util/modbus"
)

const (
	qc45RegDCActiveConnector = 4
	qc45RegType2Session      = 22
	qc45RegEnergyConnector1  = 50 // 2 registers, unsigned 32-bit Wh, high word first
	qc45RegEnergyConnector2  = 52
	qc45RegEnergyConnector3  = 54
	qc45RegDCPower            = 100
	qc45RegType2Power         = 101
	qc45RegDCBudget           = 110
	qc45RegACBudget           = 111

	// Extended read-only telemetry exported by the native-integration bridge.
	// Identifier blocks contain one length register followed by 16 registers
	// carrying up to 32 ASCII bytes (two bytes per register).
	qc45RegDCIdentifier       = 146
	qc45RegType2Identifier    = 163
	qc45IdentifierRegisters   = 17
	qc45RegDCPhaseCurrents     = 180 // 3 registers, 0.1 A: L1/L2/L3
	qc45RegType2PhaseCurrents = 183 // 3 registers, 0.1 A: L1/L2/L3
)

// EfacecQC45 implements evcc control for the Efacec QC45 through the
// embedded Modbus/TCP bridge running inside the QC45 EVCSD/Tomcat JVM.
//
// mode "dc" controls the logical DC output (CHAdeMO or CCS, mutually exclusive)
// via register 110. mode "type2" controls the 43kW Type2 output via register 111.
//
// OCPP remains responsible for authorization and transaction start/stop. evcc
// controls the available charging power through the Modbus budget registers.
type EfacecQC45 struct {
	log  *util.Logger
	conn *modbus.Connection
	mode string

	mu              sync.Mutex
	lastDCConnector uint16
}

func init() {
	registry.AddCtx("efacec-qc45", NewEfacecQC45FromConfig)
}

// NewEfacecQC45FromConfig creates an Efacec QC45 charger from generic config.
func NewEfacecQC45FromConfig(ctx context.Context, other map[string]any) (api.Charger, error) {
	cc := struct {
		URI     string
		ID      uint8
		RTU     *bool `mapstructure:"rtu"`
		Delay   time.Duration
		Timeout time.Duration
		Mode    string
	}{
		ID:   1,
		Mode: "dc",
	}

	if err := util.DecodeOther(other, &cc); err != nil {
		return nil, err
	}

	settings := modbus.TcpSettings{
		URI:     cc.URI,
		ID:      cc.ID,
		RTU:     cc.RTU,
		Delay:   cc.Delay,
		Timeout: cc.Timeout,
	}

	return NewEfacecQC45(ctx, settings, cc.Mode)
}

// NewEfacecQC45 creates an Efacec QC45 charger.
func NewEfacecQC45(ctx context.Context, settings modbus.TcpSettings, mode string) (*EfacecQC45, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "ac" {
		mode = "type2"
	}

	if mode != "dc" && mode != "type2" {
		return nil, fmt.Errorf("invalid mode %q: must be dc or type2", mode)
	}

	conn, err := settings.Connection(ctx)
	if err != nil {
		return nil, err
	}

	log := util.NewLogger("efacec-qc45")
	conn.Logger(log.TRACE)

	return &EfacecQC45{
		log:  log,
		conn: conn,
		mode: mode,
	}, nil
}

func (c *EfacecQC45) readRegister(reg uint16) (uint16, error) {
	b, err := c.conn.ReadHoldingRegisters(reg, 1)
	if err != nil {
		return 0, err
	}
	if len(b) != 2 {
		return 0, fmt.Errorf("unexpected register length: %d", len(b))
	}
	return binary.BigEndian.Uint16(b), nil
}

func (c *EfacecQC45) readUint32(reg uint16) (uint32, error) {
	b, err := c.conn.ReadHoldingRegisters(reg, 2)
	if err != nil {
		return 0, err
	}
	if len(b) != 4 {
		return 0, fmt.Errorf("unexpected register length: %d", len(b))
	}
	return binary.BigEndian.Uint32(b), nil
}

func (c *EfacecQC45) powerRegister() uint16 {
	if c.mode == "type2" {
		return qc45RegType2Power
	}
	return qc45RegDCPower
}

func (c *EfacecQC45) budgetRegister() uint16 {
	if c.mode == "type2" {
		return qc45RegACBudget
	}
	return qc45RegDCBudget
}

func (c *EfacecQC45) identifierRegister() uint16 {
	if c.mode == "type2" {
		return qc45RegType2Identifier
	}
	return qc45RegDCIdentifier
}

func (c *EfacecQC45) phaseCurrentRegister() uint16 {
	if c.mode == "type2" {
		return qc45RegType2PhaseCurrents
	}
	return qc45RegDCPhaseCurrents
}

func (c *EfacecQC45) maxPowerKW() int {
	if c.mode == "type2" {
		return 43
	}
	return 50
}

func (c *EfacecQC45) sessionActive() (bool, error) {
	if c.mode == "type2" {
		v, err := c.readRegister(qc45RegType2Session)
		return v > 0, err
	}

	v, err := c.readRegister(qc45RegDCActiveConnector)
	if err != nil {
		return false, err
	}
	if v == 1 || v == 2 {
		c.mu.Lock()
		c.lastDCConnector = v
		c.mu.Unlock()
	}
	return v > 0, nil
}

// Status implements the api.Charger interface.
// A=no active EVCSD transaction, B=transaction exists but power is zero,
// C=transaction exists and the selected output is delivering power.
func (c *EfacecQC45) Status() (api.ChargeStatus, error) {
	active, err := c.sessionActive()
	if err != nil {
		return api.StatusNone, err
	}
	if !active {
		return api.StatusA, nil
	}

	power, err := c.readRegister(c.powerRegister())
	if err != nil {
		return api.StatusNone, err
	}
	if power > 0 {
		return api.StatusC, nil
	}

	return api.StatusB, nil
}

// Enabled implements the api.Charger interface. The QC45 is reported as
// enabled only after EVCSD/OCPP has created or authorized a charging session.
// Before authorization evcc therefore shows "Ladebereit: Nein"; once the
// session is released it switches to "Ja", even while charging power is still 0.
func (c *EfacecQC45) Enabled() (bool, error) {
	return c.sessionActive()
}

// Enable implements the api.Charger interface and is intentionally a no-op.
// Authorization and transaction start/stop remain under OCPP/QC45 control.
func (c *EfacecQC45) Enable(bool) error {
	return nil
}

// MaxCurrent converts evcc's current target into the QC45's integer kW budget.
// A zero current target maps to a zero power budget; positive targets are
// clamped to the charger's supported minimum and maximum power.
func (c *EfacecQC45) MaxCurrent(current int64) error {
	kw := 0
	if current > 0 {
		kw = int(math.Ceil(float64(current) * math.Sqrt(3) * 400 / 1000))
		kw = max(5, min(c.maxPowerKW(), kw))
	}

	_, err := c.conn.WriteSingleRegister(c.budgetRegister(), uint16(kw))
	if err == nil {
		c.log.DEBUG.Printf("%s budget: %dA -> %dkW", c.mode, current, kw)
	}
	return err
}

var _ api.Charger = (*EfacecQC45)(nil)

// CurrentPower implements the api.Meter interface.
func (c *EfacecQC45) CurrentPower() (float64, error) {
	kw, err := c.readRegister(c.powerRegister())
	if err != nil {
		return 0, err
	}
	return float64(kw) * 1000, nil
}

var _ api.Meter = (*EfacecQC45)(nil)

// TotalEnergy implements api.MeterEnergy. For the logical DC charger we expose
// the sum of the cumulative CHAdeMO and CCS meter counters. This stays useful
// while the station is idle and after an evcc restart instead of falling back
// to 0 until a connector becomes active. Type2 uses its own cumulative counter.
func (c *EfacecQC45) TotalEnergy() (float64, error) {
	if c.mode == "type2" {
		wh, err := c.readUint32(qc45RegEnergyConnector3)
		if err != nil {
			return 0, err
		}
		return float64(wh) / 1000, nil
	}

	wh1, err := c.readUint32(qc45RegEnergyConnector1)
	if err != nil {
		return 0, err
	}
	wh2, err := c.readUint32(qc45RegEnergyConnector2)
	if err != nil {
		return 0, err
	}

	return float64(uint64(wh1)+uint64(wh2)) / 1000, nil
}

var _ api.MeterEnergy = (*EfacecQC45)(nil)

// Identify implements api.Identifier. The native QC45 bridge exports the
// currently known EVCSD/OCPP idTag as a length-prefixed 32-byte ASCII block.
func (c *EfacecQC45) Identify() (string, error) {
	b, err := c.conn.ReadHoldingRegisters(c.identifierRegister(), qc45IdentifierRegisters)
	if err != nil {
		return "", err
	}
	if len(b) != qc45IdentifierRegisters*2 {
		return "", fmt.Errorf("unexpected identifier block length: %d", len(b))
	}

	length := int(binary.BigEndian.Uint16(b[:2]))
	if length <= 0 {
		return "", nil
	}
	if length > 32 {
		length = 32
	}

	payload := b[2 : 2+32]
	return strings.TrimSpace(string(payload[:length])), nil
}

var _ api.Identifier = (*EfacecQC45)(nil)

// Currents implements api.PhaseCurrents. Values are exported by the native
// bridge in 0.1 A. For DC this is the three-phase AC-input equivalent derived
// from live charger power; Type2 uses the same three-phase representation.
func (c *EfacecQC45) Currents() (float64, float64, float64, error) {
	b, err := c.conn.ReadHoldingRegisters(c.phaseCurrentRegister(), 3)
	if err != nil {
		return 0, 0, 0, err
	}
	if len(b) != 6 {
		return 0, 0, 0, fmt.Errorf("unexpected phase-current block length: %d", len(b))
	}

	return float64(binary.BigEndian.Uint16(b[0:2])) / 10,
		float64(binary.BigEndian.Uint16(b[2:4])) / 10,
		float64(binary.BigEndian.Uint16(b[4:6])) / 10, nil
}

var _ api.PhaseCurrents = (*EfacecQC45)(nil)
