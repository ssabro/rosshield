// Stage D — Robots CSV import (`encoding/csv`).
//
// 표면: 패키지 레벨 함수 ParseRobotsCSV(fleetID, reader) — Service 인터페이스 외부.
// 호출자(API gateway·CLI)가 결과 [CSVRow]를 받아 각 행별로 Service.CreateRobot 호출.
//
// 표준 헤더 (Phase 1 — 영문 소문자만, 한글·다중 변형은 Phase 2+):
//
//	필수: name, host, username, authType
//	선택: port (default 22), criticality (default medium),
//	      osDistro, rosDistro, tags (semicolon `;` 구분), role
//	자격증명 (정확히 하나): password, privateKeyPem [+ privateKeyPassphrase 옵션]
//
// AuthType↔자격증명 일치는 Service.CreateRobot에서 재검증되므로 여기서는 형식만 체크.
package robot

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// CSVRow는 한 행의 파싱 결과입니다 (검증 통과한 행만).
//
// LineNumber는 1-based — 헤더가 1, 첫 데이터 행이 2.
// 호출자는 이 Request로 Service.CreateRobot을 호출합니다.
type CSVRow struct {
	LineNumber int
	Request    CreateRobotRequest
}

// ImportRowError는 한 행의 검증 실패 정보입니다.
type ImportRowError struct {
	LineNumber int
	Field      string // 필드명 또는 빈 문자열(행 전체 오류)
	Reason     string
}

// Error는 사람이 읽는 한 줄 표현을 반환합니다.
func (e ImportRowError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("line %d: %s", e.LineNumber, e.Reason)
	}
	return fmt.Sprintf("line %d: %s: %s", e.LineNumber, e.Field, e.Reason)
}

// 표준 헤더 정의.
const (
	csvColName                 = "name"
	csvColHost                 = "host"
	csvColPort                 = "port"
	csvColUsername             = "username"
	csvColAuthType             = "authType"
	csvColCriticality          = "criticality"
	csvColOSDistro             = "osDistro"
	csvColROSDistro            = "rosDistro"
	csvColTags                 = "tags"
	csvColRole                 = "role"
	csvColPassword             = "password"
	csvColPrivateKeyPEM        = "privateKeyPem"
	csvColPrivateKeyPassphrase = "privateKeyPassphrase"
)

// allowedHeaders는 CSV에서 허용되는 모든 헤더 키 집합입니다.
var allowedHeaders = map[string]struct{}{
	csvColName:                 {},
	csvColHost:                 {},
	csvColPort:                 {},
	csvColUsername:             {},
	csvColAuthType:             {},
	csvColCriticality:          {},
	csvColOSDistro:             {},
	csvColROSDistro:            {},
	csvColTags:                 {},
	csvColRole:                 {},
	csvColPassword:             {},
	csvColPrivateKeyPEM:        {},
	csvColPrivateKeyPassphrase: {},
}

// requiredHeaders는 부재 시 ErrCSVMissingHeader를 발생시키는 헤더입니다.
var requiredHeaders = []string{csvColName, csvColHost, csvColUsername, csvColAuthType}

// ParseRobotsCSV는 reader의 CSV를 파싱하여 검증된 행과 오류 행을 분리합니다.
//
// 동작:
//   - 첫 행은 헤더 (필수 컬럼 부재 시 ErrCSVMissingHeader 즉시 반환).
//   - UTF-8 BOM 자동 제거.
//   - 알 수 없는 헤더가 있으면 ErrCSVUnknownHeader.
//   - 행별 검증: 빈 필수 셀·잘못된 port·미지원 authType·자격증명 ambiguous/missing 등.
//   - 한 행 실패는 다음 행 진행 — 부분 성공 (nrobotcheck 패턴 답습).
//   - 결과: 검증 통과한 [CSVRow] + 실패한 [ImportRowError].
//   - 두 슬라이스 모두 비면 ErrCSVEmpty (헤더만 있는 경우 포함).
func ParseRobotsCSV(fleetID string, reader io.Reader) ([]CSVRow, []ImportRowError, error) {
	cr := csv.NewReader(reader)
	cr.FieldsPerRecord = -1 // 행마다 다른 컬럼 수 허용 (옵션 컬럼 빈 셀)
	cr.TrimLeadingSpace = true

	headers, err := cr.Read()
	if err != nil {
		if err == io.EOF {
			return nil, nil, ErrCSVEmpty
		}
		return nil, nil, fmt.Errorf("robot: read CSV header: %w", err)
	}
	headers = stripBOMHeaders(headers)
	headerIdx, err := buildHeaderIndex(headers)
	if err != nil {
		return nil, nil, err
	}

	var rows []CSVRow
	var errs []ImportRowError
	line := 2 // 헤더가 1, 첫 데이터 행이 2
	for {
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			errs = append(errs, ImportRowError{
				LineNumber: line,
				Reason:     fmt.Sprintf("CSV parse error: %v", err),
			})
			line++
			continue
		}
		// 모두 빈 행 skip.
		if isEmptyRecord(record) {
			line++
			continue
		}

		req, rowErrs := parseRow(line, record, headerIdx, fleetID)
		if len(rowErrs) > 0 {
			errs = append(errs, rowErrs...)
			line++
			continue
		}
		rows = append(rows, CSVRow{LineNumber: line, Request: req})
		line++
	}

	if len(rows) == 0 && len(errs) == 0 {
		return nil, nil, ErrCSVEmpty
	}
	return rows, errs, nil
}

// stripBOMHeaders는 첫 헤더 셀의 UTF-8 BOM(EF BB BF)을 제거합니다.
func stripBOMHeaders(headers []string) []string {
	if len(headers) == 0 {
		return headers
	}
	headers[0] = strings.TrimPrefix(headers[0], "\ufeff")
	return headers
}

// buildHeaderIndex는 헤더 슬라이스를 컬럼명→인덱스 맵으로 변환합니다.
//
// 알 수 없는 헤더 발견 시 ErrCSVUnknownHeader (미사용 컬럼 무시 정책 X — Phase 1은 strict).
// 필수 컬럼 부재 시 ErrCSVMissingHeader.
func buildHeaderIndex(headers []string) (map[string]int, error) {
	idx := make(map[string]int, len(headers))
	for i, raw := range headers {
		h := strings.TrimSpace(raw)
		if h == "" {
			return nil, fmt.Errorf("robot: CSV header[%d] is empty", i)
		}
		if _, ok := allowedHeaders[h]; !ok {
			return nil, fmt.Errorf("%w: %q", ErrCSVUnknownHeader, h)
		}
		if _, dup := idx[h]; dup {
			return nil, fmt.Errorf("robot: CSV header %q appears twice", h)
		}
		idx[h] = i
	}
	for _, req := range requiredHeaders {
		if _, ok := idx[req]; !ok {
			return nil, fmt.Errorf("%w: %q", ErrCSVMissingHeader, req)
		}
	}
	return idx, nil
}

// parseRow는 한 행을 CreateRobotRequest로 변환합니다 (검증 포함).
func parseRow(line int, record []string, idx map[string]int, fleetID string) (CreateRobotRequest, []ImportRowError) {
	req := CreateRobotRequest{FleetID: fleetID}
	var errs []ImportRowError

	get := func(col string) string {
		i, ok := idx[col]
		if !ok || i >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[i])
	}
	addErr := func(field, reason string) {
		errs = append(errs, ImportRowError{LineNumber: line, Field: field, Reason: reason})
	}

	req.Name = get(csvColName)
	if req.Name == "" {
		addErr(csvColName, "required")
	}
	req.Host = get(csvColHost)
	if req.Host == "" {
		addErr(csvColHost, "required")
	}

	username := get(csvColUsername)
	if username == "" {
		addErr(csvColUsername, "required")
	}

	if portStr := get(csvColPort); portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err != nil || p < 1 || p > 65535 {
			addErr(csvColPort, fmt.Sprintf("invalid port %q (must be 1..65535)", portStr))
		} else {
			req.Port = p
		}
	}

	authStr := get(csvColAuthType)
	switch authStr {
	case "password":
		req.AuthType = AuthTypePassword
	case "privateKey":
		req.AuthType = AuthTypePrivateKey
	case "":
		addErr(csvColAuthType, "required")
	default:
		addErr(csvColAuthType, fmt.Sprintf("invalid value %q (must be password or privateKey)", authStr))
	}

	password := get(csvColPassword)
	pkPEM := get(csvColPrivateKeyPEM)
	pkPass := get(csvColPrivateKeyPassphrase)
	switch {
	case password != "" && pkPEM != "":
		errs = append(errs, ImportRowError{LineNumber: line, Reason: ErrCSVCredentialAmbiguous.Error()})
	case password == "" && pkPEM == "":
		errs = append(errs, ImportRowError{LineNumber: line, Reason: ErrCSVCredentialMissing.Error()})
	}

	if crit := get(csvColCriticality); crit != "" {
		switch Criticality(crit) {
		case CriticalityLow, CriticalityMedium, CriticalityHigh, CriticalityCritical:
			req.Criticality = Criticality(crit)
		default:
			addErr(csvColCriticality, fmt.Sprintf("invalid value %q", crit))
		}
	}

	req.OSDistro = get(csvColOSDistro)
	req.ROSDistro = get(csvColROSDistro)
	req.Role = get(csvColRole)

	if tags := get(csvColTags); tags != "" {
		parts := strings.Split(tags, ";")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		req.Tags = out
	}

	// 행에 형식 오류가 있으면 Material 조립 생략 (Service.CreateRobot 호출 안 됨).
	if len(errs) > 0 {
		return CreateRobotRequest{}, errs
	}

	// Material 조립 — AuthType과 일치.
	switch req.AuthType {
	case AuthTypePassword:
		req.Material = CredentialMaterial{
			Type:     CredentialTypePassword,
			Username: username,
			Password: password,
		}
	case AuthTypePrivateKey:
		req.Material = CredentialMaterial{
			Type:                 CredentialTypePrivateKey,
			Username:             username,
			PrivateKeyPEM:        pkPEM,
			PrivateKeyPassphrase: pkPass,
		}
	}
	// AuthType↔자격증명 컬럼 일치 검증 (예: authType=password인데 privateKeyPem만 있음).
	if req.AuthType == AuthTypePassword && password == "" {
		errs = append(errs, ImportRowError{LineNumber: line, Field: csvColPassword, Reason: "required when authType=password"})
		return CreateRobotRequest{}, errs
	}
	if req.AuthType == AuthTypePrivateKey && pkPEM == "" {
		errs = append(errs, ImportRowError{LineNumber: line, Field: csvColPrivateKeyPEM, Reason: "required when authType=privateKey"})
		return CreateRobotRequest{}, errs
	}

	return req, nil
}

func isEmptyRecord(record []string) bool {
	for _, c := range record {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}
