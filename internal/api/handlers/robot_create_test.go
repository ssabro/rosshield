package handlers_test

// robot_create_test.go — A1 운영 갭 회수 통합 테스트.
//
// 시나리오:
//  1. POST /robots → 201 + robot 메타 + credentialId
//  2. fleet 미존재 → 400 (ErrFleetNotFound)
//  3. name 누락 → 400
//  4. auth 누락 → 401
//  5. 같은 fleet에 같은 name 두 번 → 두 번째 409 (ErrRobotNameDuplicate)

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/robot"
	"github.com/ssabro/rosshield/internal/platform/storage"
)

// seedFleet은 robot 없이 fleet만 생성합니다 (CreateRobot 테스트용).
func seedFleetOnly(t *testing.T, f *testFixture, name string) string {
	t.Helper()
	ctx := storage.WithTenantID(context.Background(), f.tenantID)
	var fleetID string
	if err := f.storage.Tx(ctx, func(ctx context.Context, tx storage.Tx) error {
		fl, e := f.robot.CreateFleet(ctx, tx, robot.CreateFleetRequest{Name: name})
		if e != nil {
			return e
		}
		fleetID = fl.ID
		return nil
	}); err != nil {
		t.Fatalf("seedFleet: %v", err)
	}
	return fleetID
}

func TestCreateRobotReturns201(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	fleetID := seedFleetOnly(t, f, "fleet-create-1")
	token := f.loginAndGetToken(t)

	body, _ := json.Marshal(map[string]any{
		"fleetId":     fleetID,
		"name":        "robot-new-1",
		"host":        "10.0.99.1",
		"port":        22,
		"authType":    "password",
		"username":    "ros",
		"password":    "topsecret123",
		"criticality": "high",
		"tags":        []string{"prod", "kr"},
	})
	resp := f.doRequest(t, "POST", "/api/v1/robots", token, body)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		Robot struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Host        string `json:"host"`
			Criticality string `json:"criticality"`
		} `json:"robot"`
		CredentialID string `json:"credentialId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(out.Robot.ID, "ro_") {
		t.Errorf("robot.id=%q, want prefix ro_", out.Robot.ID)
	}
	if out.Robot.Name != "robot-new-1" || out.Robot.Host != "10.0.99.1" {
		t.Errorf("name/host mismatch: %+v", out.Robot)
	}
	if out.Robot.Criticality != "high" {
		t.Errorf("criticality=%q, want high", out.Robot.Criticality)
	}
	if !strings.HasPrefix(out.CredentialID, "cr_") {
		t.Errorf("credentialId=%q, want prefix cr_", out.CredentialID)
	}
}

func TestCreateRobot400ForUnknownFleet(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	token := f.loginAndGetToken(t)
	body, _ := json.Marshal(map[string]any{
		"fleetId":  "fl_NOTEXIST",
		"name":     "x",
		"host":     "10.0.0.1",
		"port":     22,
		"authType": "password",
		"username": "u",
		"password": "p",
	})
	resp := f.doRequest(t, "POST", "/api/v1/robots", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, want 400; body=%s", resp.StatusCode, string(raw))
	}
}

func TestCreateRobot400ForEmptyName(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	fleetID := seedFleetOnly(t, f, "fleet-create-2")
	token := f.loginAndGetToken(t)

	body, _ := json.Marshal(map[string]any{
		"fleetId":  fleetID,
		"host":     "10.0.0.2",
		"port":     22,
		"authType": "password",
		"username": "u",
		"password": "p",
	})
	resp := f.doRequest(t, "POST", "/api/v1/robots", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, want 400; body=%s", resp.StatusCode, string(raw))
	}
}

func TestCreateRobot401WithoutAuth(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	body, _ := json.Marshal(map[string]any{"name": "x"})
	resp := f.doRequest(t, "POST", "/api/v1/robots", "", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}

func TestCreateRobot409ForDuplicateNameInFleet(t *testing.T) {
	f := newFixture(t)
	defer f.closeFn()

	fleetID := seedFleetOnly(t, f, "fleet-dup")
	token := f.loginAndGetToken(t)
	mkBody := func(name string) []byte {
		b, _ := json.Marshal(map[string]any{
			"fleetId":  fleetID,
			"name":     name,
			"host":     "10.0.0.3",
			"port":     22,
			"authType": "password",
			"username": "u",
			"password": "p",
		})
		return b
	}

	// 첫 번째 — 201.
	resp1 := f.doRequest(t, "POST", "/api/v1/robots", token, mkBody("dup-name"))
	_ = resp1.Body.Close()
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("first create status=%d, want 201", resp1.StatusCode)
	}

	// 두 번째 — 409 NameDuplicate.
	resp2 := f.doRequest(t, "POST", "/api/v1/robots", token, mkBody("dup-name"))
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp2.Body)
		t.Fatalf("second create status=%d, want 409; body=%s", resp2.StatusCode, string(raw))
	}
}
