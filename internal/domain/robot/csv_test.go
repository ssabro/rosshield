package robot_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ssabro/rosshield/internal/domain/robot"
)

const csvHeader = "name,host,port,username,authType,criticality,osDistro,rosDistro,tags,role,password,privateKeyPem,privateKeyPassphrase"

func TestParseRobotsCSVAcceptsValidRows(t *testing.T) {
	t.Parallel()
	body := csvHeader + "\n" +
		"bot01,10.0.0.10,22,rosshield,privateKey,high,ubuntu-24.04,jazzy,prod;indoor,mobile,,bot01-pk-singleline,passphrase\n" +
		"bot02,10.0.0.11,2222,rosshield,password,medium,ubuntu-22.04,humble,test,manipulator,bot02-pw,,\n"

	rows, errs, err := robot.ParseRobotsCSV("fl_TEST", strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseRobotsCSV: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("got %d errors, want 0: %v", len(errs), errs)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}

	r1 := rows[0].Request
	if r1.Name != "bot01" || r1.Host != "10.0.0.10" || r1.Port != 22 {
		t.Errorf("row1 base = %+v", r1)
	}
	if r1.AuthType != robot.AuthTypePrivateKey {
		t.Errorf("row1 authType = %q, want privateKey", r1.AuthType)
	}
	if r1.Criticality != robot.CriticalityHigh {
		t.Errorf("row1 criticality = %q, want high", r1.Criticality)
	}
	if r1.Material.Type != robot.CredentialTypePrivateKey {
		t.Errorf("row1 material type = %q, want privateKey", r1.Material.Type)
	}
	if r1.Material.PrivateKeyPassphrase != "passphrase" {
		t.Errorf("row1 passphrase = %q", r1.Material.PrivateKeyPassphrase)
	}
	if len(r1.Tags) != 2 || r1.Tags[0] != "prod" || r1.Tags[1] != "indoor" {
		t.Errorf("row1 tags = %+v, want [prod indoor]", r1.Tags)
	}

	r2 := rows[1].Request
	if r2.Name != "bot02" || r2.Port != 2222 {
		t.Errorf("row2 = %+v", r2)
	}
	if r2.AuthType != robot.AuthTypePassword {
		t.Errorf("row2 authType = %q, want password", r2.AuthType)
	}
	if r2.Material.Password != "bot02-pw" {
		t.Errorf("row2 password = %q", r2.Material.Password)
	}
}

// E5.T6 — TestRobotCSVImportValidates 핵심: 잘못된 IP·포트·자격증명 거부.
func TestParseRobotsCSVRejectsInvalidRows(t *testing.T) {
	t.Parallel()
	body := csvHeader + "\n" +
		// 1: 빈 host
		"bot01,,22,rosshield,privateKey,,,,,,,pk,\n" +
		// 2: port 99999 (out of range)
		"bot02,10.0.0.2,99999,rosshield,password,,,,,,pw,,\n" +
		// 3: port non-numeric
		"bot03,10.0.0.3,xx,rosshield,password,,,,,,pw,,\n" +
		// 4: authType invalid
		"bot04,10.0.0.4,22,rosshield,oauth,,,,,,pw,,\n" +
		// 5: 자격증명 없음
		"bot05,10.0.0.5,22,rosshield,password,,,,,,,,\n" +
		// 6: password + privateKeyPem 동시
		"bot06,10.0.0.6,22,rosshield,privateKey,,,,,,pw,pk,\n" +
		// 7: authType=password인데 privateKeyPem만
		"bot07,10.0.0.7,22,rosshield,password,,,,,,,pk,\n" +
		// 8: 빈 username
		"bot08,10.0.0.8,22,,privateKey,,,,,,,pk,\n" +
		// 9: criticality 잘못된 값
		"bot09,10.0.0.9,22,rosshield,privateKey,ultra,,,,,,pk,\n" +
		// 10: 정상 — 부분 성공 검증
		"bot10,10.0.0.10,22,rosshield,privateKey,,,,,,,pk,\n"

	rows, errs, err := robot.ParseRobotsCSV("fl_TEST", strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseRobotsCSV: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("valid rows = %d, want 1 (only bot10): %+v", len(rows), rows)
	} else if rows[0].Request.Name != "bot10" {
		t.Errorf("only valid row should be bot10, got %q", rows[0].Request.Name)
	}
	// 9개 행 거부 — 일부는 여러 에러가 한 행에 있을 수 있어 정확한 카운트보다 ≥9 검증.
	if len(errs) < 9 {
		t.Errorf("error count = %d, want ≥9: %v", len(errs), errs)
	}

	// 행 번호별 첫 에러 확인.
	mustHaveErrAt := func(line int, fieldHint string) {
		t.Helper()
		for _, e := range errs {
			if e.LineNumber == line && (fieldHint == "" || e.Field == fieldHint || strings.Contains(e.Reason, fieldHint)) {
				return
			}
		}
		t.Errorf("no error at line %d (field/reason hint=%q): errs=%v", line, fieldHint, errs)
	}
	mustHaveErrAt(2, "host")
	mustHaveErrAt(3, "port")
	mustHaveErrAt(4, "port")
	mustHaveErrAt(5, "authType")
	mustHaveErrAt(6, "")
	mustHaveErrAt(7, "")
	mustHaveErrAt(8, "")
	mustHaveErrAt(9, "username")
	mustHaveErrAt(10, "criticality")
}

func TestParseRobotsCSVRejectsMissingHeader(t *testing.T) {
	t.Parallel()
	// authType 누락
	body := "name,host,username\nbot01,10.0.0.1,rosshield\n"
	_, _, err := robot.ParseRobotsCSV("fl_TEST", strings.NewReader(body))
	if !errors.Is(err, robot.ErrCSVMissingHeader) {
		t.Errorf("err = %v, want ErrCSVMissingHeader", err)
	}
}

func TestParseRobotsCSVRejectsUnknownHeader(t *testing.T) {
	t.Parallel()
	body := "name,host,username,authType,foo\nbot01,10.0.0.1,rosshield,privateKey,bar\n"
	_, _, err := robot.ParseRobotsCSV("fl_TEST", strings.NewReader(body))
	if !errors.Is(err, robot.ErrCSVUnknownHeader) {
		t.Errorf("err = %v, want ErrCSVUnknownHeader", err)
	}
}

func TestParseRobotsCSVRejectsEmpty(t *testing.T) {
	t.Parallel()
	_, _, err := robot.ParseRobotsCSV("fl_TEST", strings.NewReader(""))
	if !errors.Is(err, robot.ErrCSVEmpty) {
		t.Errorf("err = %v, want ErrCSVEmpty", err)
	}
}

func TestParseRobotsCSVHeaderOnlyIsEmpty(t *testing.T) {
	t.Parallel()
	body := csvHeader + "\n"
	_, _, err := robot.ParseRobotsCSV("fl_TEST", strings.NewReader(body))
	if !errors.Is(err, robot.ErrCSVEmpty) {
		t.Errorf("err = %v, want ErrCSVEmpty for header-only", err)
	}
}

func TestParseRobotsCSVStripsBOM(t *testing.T) {
	t.Parallel()
	// UTF-8 BOM (EF BB BF)
	body := "\ufeff" + csvHeader + "\n" +
		"bot01,10.0.0.10,22,rosshield,privateKey,,,,,,,pk,\n"
	rows, errs, err := robot.ParseRobotsCSV("fl_TEST", strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseRobotsCSV: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want 0", errs)
	}
	if len(rows) != 1 || rows[0].Request.Name != "bot01" {
		t.Errorf("rows = %+v, want 1 row bot01", rows)
	}
}

func TestParseRobotsCSVAppliesFleetID(t *testing.T) {
	t.Parallel()
	body := csvHeader + "\nbot01,10.0.0.10,22,rosshield,privateKey,,,,,,,pk,\n"
	const fleetID = "fl_FROMARG"
	rows, _, err := robot.ParseRobotsCSV(fleetID, strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseRobotsCSV: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows", len(rows))
	}
	if rows[0].Request.FleetID != fleetID {
		t.Errorf("FleetID = %q, want %q", rows[0].Request.FleetID, fleetID)
	}
}

func TestParseRobotsCSVSkipsBlankLines(t *testing.T) {
	t.Parallel()
	body := csvHeader + "\n" +
		"\n" + // 빈 행
		"  ,  ,  ,  ,  ,  ,  ,  ,  ,  ,  ,  ,  \n" + // 모두 공백
		"bot01,10.0.0.10,22,rosshield,privateKey,,,,,,,pk,\n"
	rows, errs, err := robot.ParseRobotsCSV("fl_TEST", strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseRobotsCSV: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want 0", errs)
	}
	if len(rows) != 1 {
		t.Errorf("rows = %d, want 1 (skips blanks)", len(rows))
	}
}

// 결과 행의 LineNumber가 정확.
func TestParseRobotsCSVLineNumber(t *testing.T) {
	t.Parallel()
	body := csvHeader + "\n" +
		"bot01,10.0.0.10,22,rosshield,privateKey,,,,,,,pk,\n" +
		"bot02,,22,rosshield,privateKey,,,,,,,pk,\n" + // 빈 host → error at line 3
		"bot03,10.0.0.30,22,rosshield,privateKey,,,,,,,pk,\n"

	rows, errs, err := robot.ParseRobotsCSV("fl_TEST", strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseRobotsCSV: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("rows = %d, want 2", len(rows))
	}
	if rows[0].LineNumber != 2 {
		t.Errorf("rows[0] LineNumber = %d, want 2", rows[0].LineNumber)
	}
	if rows[1].LineNumber != 4 {
		t.Errorf("rows[1] LineNumber = %d, want 4", rows[1].LineNumber)
	}
	if len(errs) != 1 || errs[0].LineNumber != 3 {
		t.Errorf("errs = %v, want 1 error at line 3", errs)
	}
}

func TestImportRowErrorString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  robot.ImportRowError
		want string
	}{
		{robot.ImportRowError{LineNumber: 5, Field: "port", Reason: "invalid"}, "line 5: port: invalid"},
		{robot.ImportRowError{LineNumber: 7, Reason: "credential ambiguous"}, "line 7: credential ambiguous"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("Error() = %q, want %q", got, c.want)
		}
	}
}
