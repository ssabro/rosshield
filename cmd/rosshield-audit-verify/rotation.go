// rotation.go — Stage 5: audit rotation segment archive 외부 검증 서브커맨드.
//
// 본 파일은 `rosshield-audit-verify rotation` 서브커맨드를 구현합니다. 외부 감사인이
// release page 또는 customer 환경에서 받은 cold archive (tar.gz)의 무결성을 단독 검증할 수 있게
// 합니다 — rosshield-server·DB 접근 불필요. stdlib + 도메인 hash 함수만 의존.
//
// 두 가지 모드:
//
//	rosshield-audit-verify rotation \
//	  --archive-uri file:///path/to/seg-000005.tar.gz \
//	  --expected-sha256 <hex64> \
//	  --expected-segment-hash <hex64> \
//	  [--prev-segment-hash <hex64>]
//
//	rosshield-audit-verify rotation chain \
//	  --backend file:///path/to/audit-archives/<tenant>/ \
//	  --from-segment 1 --to-segment 10
//
// 첫 형식은 single archive verify (감사인이 외부 채널에서 expected sha256/segment_hash 받은 경우).
// 두 번째는 backend 내 연속 segment 들의 chain 일관성 batch verify.
//
// 검증 순서 (single):
//  1. archive bytes fetch (file://).
//  2. sha256(body) == expected-sha256 (옵션).
//  3. tar.gz unwrap → manifest.json + entries.ndjson.
//  4. entries.ndjson 파싱 → audit.Entry 슬라이스 → ComputeSegmentHash 재계산.
//  5. 재계산 hash == manifest.segmentHash == expected-segment-hash (옵션).
//  6. manifest.prevSegmentHash == expected-prev (옵션).
//
// chain mode는 위 절차를 from~to 순차 반복 + 매 step의 manifest.segmentHash를
// 다음 step의 expected-prev로 자동 forward — 외부 audit fixture 없이도 self-consistent
// chain check 가능.

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ssabro/rosshield/internal/domain/audit"
	"github.com/ssabro/rosshield/internal/domain/audit/rotation"
)

// cosignConfig는 rotation verify 단계에서 cosign verify-blob 호출 옵션을 묶습니다.
//
// design doc D-AR-4의 sign 측 옵션 A(외부 cosign CLI)와 일관 — verify CLI는 stdlib만
// 의존하는 게 원칙이므로 sigstore-go SDK 의존을 피하고 `cosign verify-blob`을 spawn.
//
// 활성 조건: Identity가 비어있지 않으면 활성(bundle 경로 부재 시 FAIL). 둘 다 빈
// 값이면 skip — 기존 archive sha256 + segment_hash + prev chain 검증만 수행.
//
// chain mode(`rotation chain`)에선 BundlePath 대신 BundleDir만 지정하고 각 segment의
// bundle을 자동 검색합니다(seg-NNNNNN.cosign.bundle 명명 규약).
type cosignConfig struct {
	BundlePath string // single mode bundle 파일 경로
	BundleDir  string // chain mode bundle 디렉터리 (auto detect)
	Identity   string // --certificate-identity
	OIDCIssuer string // --certificate-oidc-issuer
	Binary     string // cosign binary path (default "cosign")
	RekorURL   string // --rekor-url (선택)
	SegmentNum int64  // chain mode 각 segment에서 채워짐 — bundle path resolve용
}

// active는 cosign verify 단계를 실행해야 하는지 리턴합니다.
//
// Identity 또는 BundlePath 둘 중 하나라도 있으면 활성 — 외부 감사인은 보통 둘 다 함께
// 받지만 신뢰 채널로 identity만 받은 경우 verify CLI가 bundle을 자동 찾도록 허용.
func (c cosignConfig) active() bool {
	return c.Identity != "" || c.BundlePath != "" || c.BundleDir != ""
}

// cosignRunArgs는 runCosignVerify에 전달되는 인자 묶음 (test 가독성 + swap 용이).
type cosignRunArgs struct {
	Binary     string
	BundlePath string
	Identity   string
	OIDCIssuer string
	RekorURL   string
	Archive    []byte
}

// cosignRunner는 cosign verify-blob 호출 함수 타입 — test에서 swap 가능.
type cosignRunner func(args cosignRunArgs) error

// runCosignVerify는 실제 `cosign verify-blob` 외부 호출을 수행합니다.
//
// 본 변수는 test에서 fake로 swap되어 외부 cosign binary 의존을 회피합니다.
// 운영 binary는 default 구현(실 cosign exec)을 사용 — 본 패키지는 cosign 의존 0.
var runCosignVerify cosignRunner = func(a cosignRunArgs) error {
	binary := a.Binary
	if binary == "" {
		binary = "cosign"
	}
	args := []string{"verify-blob", "--bundle=" + a.BundlePath}
	if a.Identity != "" {
		args = append(args, "--certificate-identity="+a.Identity)
	}
	if a.OIDCIssuer != "" {
		args = append(args, "--certificate-oidc-issuer="+a.OIDCIssuer)
	}
	if a.RekorURL != "" {
		args = append(args, "--rekor-url="+a.RekorURL)
	}
	args = append(args, "-")

	cmd := exec.CommandContext(context.Background(), binary, args...)
	cmd.Stdin = bytes.NewReader(a.Archive)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cosign verify-blob: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// rotationOutput은 rotation verify 결과 와이어 형식 (single archive).
type rotationOutput struct {
	OK                 bool         `json:"ok"`
	Result             string       `json:"result"` // "PASS" | "FAIL"
	Reason             string       `json:"reason,omitempty"`
	ArchiveURI         string       `json:"archiveUri"`
	ArchiveSHA256Match bool         `json:"archiveSha256Match"`
	ArchiveSHA256      string       `json:"archiveSha256"`
	SegmentHashMatch   bool         `json:"segmentHashMatch"`
	SegmentHash        string       `json:"segmentHash"`
	PrevChainMatch     bool         `json:"prevChainMatch"`
	PrevSegmentHash    string       `json:"prevSegmentHash,omitempty"`
	EntryCount         int64        `json:"entryCount"`
	SegmentNumber      int64        `json:"segmentNumber,omitempty"`
	ManifestVersion    string       `json:"manifestVersion"`
	CosignVerifyMatch  bool         `json:"cosignVerifyMatch"` // cosign 비활성 시 true(skip)
	CosignBundlePath   string       `json:"cosignBundlePath,omitempty"`
	Steps              []stepResult `json:"steps"`
}

// chainOutput은 batch chain verify 결과.
type chainOutput struct {
	OK           bool             `json:"ok"`
	Result       string           `json:"result"` // "PASS" | "FAIL"
	Reason       string           `json:"reason,omitempty"`
	FromSegment  int64            `json:"fromSegment"`
	ToSegment    int64            `json:"toSegment"`
	Backend      string           `json:"backend"`
	Verified     int              `json:"verified"` // 통과한 segment 개수
	FirstFailure int64            `json:"firstFailure,omitempty"`
	Segments     []rotationOutput `json:"segments"`
}

// runRotation은 `rotation` 서브커맨드 진입. args는 'rotation' 이후 토큰들.
//
// args[0]가 'chain'이면 batch mode, 그 외는 single mode.
func runRotation(args []string) int {
	if len(args) > 0 && args[0] == "chain" {
		return runRotationChain(args[1:])
	}
	return runRotationSingle(args)
}

func runRotationSingle(args []string) int {
	fs := flag.NewFlagSet("rosshield-audit-verify rotation", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archiveURI := fs.String("archive-uri", "", "검증할 segment archive URI (file://...) — 필수")
	expectedSHA := fs.String("expected-sha256", "", "archive 본문 sha256 hex (옵션; 외부 채널로 받음)")
	expectedSegHash := fs.String("expected-segment-hash", "", "manifest segment_hash hex (옵션)")
	expectedPrev := fs.String("prev-segment-hash", "", "manifest prev_segment_hash hex (옵션; 첫 segment 검증 시 생략)")
	cosignBundle := fs.String("cosign-bundle", "", "cosign signature bundle 파일 경로 (옵션; identity와 함께 활성화)")
	cosignIdentity := fs.String("cosign-identity", "", "cosign certificate-identity (예: ci@example.com 또는 regex)")
	cosignOIDCIssuer := fs.String("cosign-oidc-issuer", "", "cosign certificate-oidc-issuer (예: https://accounts.google.com)")
	cosignBinary := fs.String("cosign-binary", "", "cosign binary 경로 (default: PATH의 'cosign')")
	cosignRekorURL := fs.String("cosign-rekor-url", "", "cosign --rekor-url (옵션, default Sigstore public)")
	format := fs.String("format", "table", "table | json")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield-audit-verify: rotation flag parse: %v\n", err)
		rotationUsage()
		return 2
	}
	if *archiveURI == "" {
		fmt.Fprintln(os.Stderr, "rosshield-audit-verify: rotation --archive-uri required")
		rotationUsage()
		return 2
	}
	if *format != "table" && *format != "json" {
		fmt.Fprintf(os.Stderr, "rosshield-audit-verify: unknown --format %q\n", *format)
		return 2
	}

	cs := cosignConfig{
		BundlePath: *cosignBundle,
		Identity:   *cosignIdentity,
		OIDCIssuer: *cosignOIDCIssuer,
		Binary:     *cosignBinary,
		RekorURL:   *cosignRekorURL,
	}

	out := verifyOneArchive(*archiveURI, *expectedSHA, *expectedSegHash, *expectedPrev, cs)
	emitRotationOutput(*format, out)
	if !out.OK {
		return 1
	}
	return 0
}

func runRotationChain(args []string) int {
	fs := flag.NewFlagSet("rosshield-audit-verify rotation chain", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	backend := fs.String("backend", "", "backend root URI (file:///path/to/audit-archives/<tenant>/) — 필수")
	from := fs.Int64("from-segment", 1, "검증 시작 segmentNumber")
	to := fs.Int64("to-segment", 0, "검증 종료 segmentNumber (포함). 0이면 from 만 검증")
	cosignBundleDir := fs.String("cosign-bundle-dir", "", "chain mode bundle 디렉터리 (각 segment seg-NNNNNN.cosign.bundle 자동 검색)")
	cosignIdentity := fs.String("cosign-identity", "", "cosign certificate-identity (모든 segment 공통)")
	cosignOIDCIssuer := fs.String("cosign-oidc-issuer", "", "cosign certificate-oidc-issuer")
	cosignBinary := fs.String("cosign-binary", "", "cosign binary 경로 (default: PATH의 'cosign')")
	cosignRekorURL := fs.String("cosign-rekor-url", "", "cosign --rekor-url (옵션, default Sigstore public)")
	format := fs.String("format", "table", "table | json")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield-audit-verify: rotation chain flag parse: %v\n", err)
		rotationUsage()
		return 2
	}
	if *backend == "" {
		fmt.Fprintln(os.Stderr, "rosshield-audit-verify: rotation chain --backend required")
		rotationUsage()
		return 2
	}
	if *from <= 0 {
		fmt.Fprintln(os.Stderr, "rosshield-audit-verify: --from-segment must be > 0")
		return 2
	}
	if *to == 0 {
		*to = *from
	}
	if *to < *from {
		fmt.Fprintln(os.Stderr, "rosshield-audit-verify: --to-segment must be >= --from-segment")
		return 2
	}
	if *format != "table" && *format != "json" {
		fmt.Fprintf(os.Stderr, "rosshield-audit-verify: unknown --format %q\n", *format)
		return 2
	}

	out := chainOutput{
		FromSegment: *from,
		ToSegment:   *to,
		Backend:     *backend,
	}

	csBase := cosignConfig{
		BundleDir:  *cosignBundleDir,
		Identity:   *cosignIdentity,
		OIDCIssuer: *cosignOIDCIssuer,
		Binary:     *cosignBinary,
		RekorURL:   *cosignRekorURL,
	}

	var prevHashHex string // segment 1의 prev는 ""; 이후 단계에서 forward.
	for n := *from; n <= *to; n++ {
		uri, err := segmentArchiveURI(*backend, n)
		if err != nil {
			out.OK = false
			out.Result = "FAIL"
			out.Reason = fmt.Sprintf("backend URI build seg=%d: %v", n, err)
			out.FirstFailure = n
			emitChainOutput(*format, out)
			return 1
		}
		// 첫 segment 또는 from > 1로 시작하는 경우: prevHash 미상이면 manifest의 값을 신뢰 (chain self-consistent check만).
		expectedPrev := prevHashHex
		if n == *from && n > 1 {
			// chain check 시작점 — manifest 값으로 prev forward 시작.
			expectedPrev = ""
		}
		cs := csBase
		cs.SegmentNum = n
		if cs.BundleDir != "" {
			cs.BundlePath = filepath.Join(cs.BundleDir, fmt.Sprintf("seg-%06d.cosign.bundle", n))
		}
		segOut := verifyOneArchive(uri, "", "", expectedPrev, cs)
		segOut.SegmentNumber = n
		out.Segments = append(out.Segments, segOut)
		if !segOut.OK {
			out.Result = "FAIL"
			out.Reason = segOut.Reason
			out.FirstFailure = n
			emitChainOutput(*format, out)
			return 1
		}
		out.Verified++
		// 다음 step의 expected-prev로 본 segment의 segment_hash forward.
		prevHashHex = segOut.SegmentHash
	}
	out.OK = true
	out.Result = "PASS"
	emitChainOutput(*format, out)
	return 0
}

// verifyOneArchive는 한 segment archive를 검증해 rotationOutput을 반환합니다.
//
// expectedSHA / expectedSegHash / expectedPrev는 빈 문자열이면 skip (해당 단계 무조건 match=true).
// cs.active()=true면 cosign verify-blob step도 수행 — bundle 파일 부재 또는 runner error → 전체 FAIL.
func verifyOneArchive(archiveURI, expectedSHA, expectedSegHash, expectedPrev string, cs cosignConfig) rotationOutput {
	out := rotationOutput{ArchiveURI: archiveURI}

	body, err := fetchArchive(archiveURI)
	if err != nil {
		out.Result = "FAIL"
		out.Reason = fmt.Sprintf("fetch: %v", err)
		out.Steps = []stepResult{{Name: "fetch", OK: false, Detail: err.Error()}}
		return out
	}
	out.Steps = append(out.Steps, stepResult{Name: "fetch", OK: true,
		Detail: fmt.Sprintf("%d bytes", len(body))})

	sum := sha256.Sum256(body)
	out.ArchiveSHA256 = hex.EncodeToString(sum[:])
	if expectedSHA != "" {
		if !strings.EqualFold(out.ArchiveSHA256, expectedSHA) {
			out.Result = "FAIL"
			out.Reason = fmt.Sprintf("archive sha256 mismatch (got %s, want %s)",
				out.ArchiveSHA256, expectedSHA)
			out.Steps = append(out.Steps, stepResult{Name: "archiveSha256", OK: false, Detail: out.Reason})
			return out
		}
		out.ArchiveSHA256Match = true
		out.Steps = append(out.Steps, stepResult{Name: "archiveSha256", OK: true,
			Detail: "matches expected"})
	} else {
		out.ArchiveSHA256Match = true // skip → true
		out.Steps = append(out.Steps, stepResult{Name: "archiveSha256", OK: true,
			Detail: "computed (no expected — skip compare)"})
	}

	manifest, entries, err := readSegmentArchive(body)
	if err != nil {
		out.Result = "FAIL"
		out.Reason = fmt.Sprintf("archive parse: %v", err)
		out.Steps = append(out.Steps, stepResult{Name: "extract", OK: false, Detail: err.Error()})
		return out
	}
	out.ManifestVersion = manifest.Version
	out.EntryCount = manifest.EntryCount
	out.SegmentHash = manifest.SegmentHash
	out.PrevSegmentHash = manifest.PrevSegmentHash
	out.Steps = append(out.Steps, stepResult{Name: "extract", OK: true,
		Detail: fmt.Sprintf("manifest v%s, %d entries", manifest.Version, manifest.EntryCount)})

	// entries로 segment_hash 재계산.
	recomputed := rotation.ComputeSegmentHash(entries)
	recomputedHex := hex.EncodeToString(recomputed[:])
	if !strings.EqualFold(recomputedHex, manifest.SegmentHash) {
		out.Result = "FAIL"
		out.Reason = fmt.Sprintf("segment_hash mismatch (recomputed %s, manifest %s)",
			recomputedHex, manifest.SegmentHash)
		out.Steps = append(out.Steps, stepResult{Name: "segmentHash", OK: false, Detail: out.Reason})
		return out
	}
	if expectedSegHash != "" && !strings.EqualFold(manifest.SegmentHash, expectedSegHash) {
		out.Result = "FAIL"
		out.Reason = fmt.Sprintf("segment_hash != expected (manifest %s, expected %s)",
			manifest.SegmentHash, expectedSegHash)
		out.Steps = append(out.Steps, stepResult{Name: "segmentHash", OK: false, Detail: out.Reason})
		return out
	}
	out.SegmentHashMatch = true
	out.Steps = append(out.Steps, stepResult{Name: "segmentHash", OK: true,
		Detail: "recomputed == manifest" + ifExpected(expectedSegHash, " == expected")})

	// prev_segment_hash chain link.
	if expectedPrev != "" && !strings.EqualFold(manifest.PrevSegmentHash, expectedPrev) {
		out.Result = "FAIL"
		out.Reason = fmt.Sprintf("prev_segment_hash mismatch (manifest %s, expected %s)",
			manifest.PrevSegmentHash, expectedPrev)
		out.Steps = append(out.Steps, stepResult{Name: "prevChain", OK: false, Detail: out.Reason})
		return out
	}
	out.PrevChainMatch = true
	if expectedPrev != "" {
		out.Steps = append(out.Steps, stepResult{Name: "prevChain", OK: true,
			Detail: "matches expected prev_segment_hash"})
	} else {
		out.Steps = append(out.Steps, stepResult{Name: "prevChain", OK: true,
			Detail: "no expected — skip compare"})
	}

	// cosign verify-blob step (옵션). cs.active()=false면 skip + CosignVerifyMatch=true.
	if !cs.active() {
		out.CosignVerifyMatch = true
		out.Steps = append(out.Steps, stepResult{Name: "cosignVerify", OK: true,
			Detail: "skipped (no --cosign-identity/--cosign-bundle)"})
	} else {
		bundlePath := cs.BundlePath
		out.CosignBundlePath = bundlePath
		if bundlePath == "" {
			out.Result = "FAIL"
			out.Reason = "cosign verify requested but --cosign-bundle empty"
			out.Steps = append(out.Steps, stepResult{Name: "cosignVerify", OK: false, Detail: out.Reason})
			return out
		}
		if _, statErr := os.Stat(bundlePath); statErr != nil {
			out.Result = "FAIL"
			out.Reason = fmt.Sprintf("cosign bundle missing: %v", statErr)
			out.Steps = append(out.Steps, stepResult{Name: "cosignVerify", OK: false, Detail: out.Reason})
			return out
		}
		runErr := runCosignVerify(cosignRunArgs{
			Binary:     cs.Binary,
			BundlePath: bundlePath,
			Identity:   cs.Identity,
			OIDCIssuer: cs.OIDCIssuer,
			RekorURL:   cs.RekorURL,
			Archive:    body,
		})
		if runErr != nil {
			out.Result = "FAIL"
			out.Reason = runErr.Error()
			out.Steps = append(out.Steps, stepResult{Name: "cosignVerify", OK: false, Detail: runErr.Error()})
			return out
		}
		out.CosignVerifyMatch = true
		out.Steps = append(out.Steps, stepResult{Name: "cosignVerify", OK: true,
			Detail: "cosign verify-blob OK (bundle=" + bundlePath + ")"})
	}

	out.OK = true
	out.Result = "PASS"
	return out
}

// segmentManifest는 archive manifest.json의 v2 형식 (PrevSegmentHash 포함).
type segmentManifest struct {
	Version         string `json:"version"`
	TenantID        string `json:"tenantId"`
	FirstEntryID    int64  `json:"firstEntryId"`
	LastEntryID     int64  `json:"lastEntryId"`
	EntryCount      int64  `json:"entryCount"`
	StartedAt       string `json:"startedAt"`
	EndedAt         string `json:"endedAt"`
	SegmentHash     string `json:"segmentHash"`
	PrevSegmentHash string `json:"prevSegmentHash,omitempty"`
	EntriesFile     string `json:"entriesFile"`
	CreatedAt       string `json:"createdAt"`
}

// readSegmentArchive는 tar.gz body → (manifest, entries).
//
// entries는 NDJSON 라인을 audit.UnmarshalEntryLine로 디코드.
func readSegmentArchive(body []byte) (segmentManifest, []audit.Entry, error) {
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return segmentManifest{}, nil, fmt.Errorf("gzip open: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	var manifestJSON []byte
	var entriesNDJSON []byte
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return segmentManifest{}, nil, fmt.Errorf("tar next: %w", err)
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return segmentManifest{}, nil, fmt.Errorf("read entry %q: %w", hdr.Name, err)
		}
		switch hdr.Name {
		case "manifest.json":
			manifestJSON = data
		case "entries.ndjson":
			entriesNDJSON = data
		}
	}
	if manifestJSON == nil {
		return segmentManifest{}, nil, errors.New("manifest.json missing in archive")
	}
	if entriesNDJSON == nil {
		return segmentManifest{}, nil, errors.New("entries.ndjson missing in archive")
	}

	var m segmentManifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		return segmentManifest{}, nil, fmt.Errorf("manifest json decode: %w", err)
	}

	entries, err := parseEntries(entriesNDJSON)
	if err != nil {
		return segmentManifest{}, nil, err
	}
	if int64(len(entries)) != m.EntryCount {
		return segmentManifest{}, nil, fmt.Errorf("entries count mismatch: file=%d, manifest=%d",
			len(entries), m.EntryCount)
	}
	return m, entries, nil
}

// parseEntries는 NDJSON bytes를 audit.Entry 슬라이스로 변환합니다.
func parseEntries(ndjson []byte) ([]audit.Entry, error) {
	var entries []audit.Entry
	for i, line := range bytes.Split(bytes.TrimRight(ndjson, "\n"), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		e, err := audit.UnmarshalEntryLine(line)
		if err != nil {
			return nil, fmt.Errorf("entries.ndjson line %d: %w", i+1, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// fetchArchive는 archive URI에서 본문 bytes를 가져옵니다.
//
// 현재 file:// 만 지원 (외부 감사인 사용 케이스의 99%).
// s3:// 등은 BSL backend module 의존이라 별 epic.
func fetchArchive(uri string) ([]byte, error) {
	path, err := fileURIToPath(uri)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// fileURIToPath는 file:// URI → OS native 경로 (Windows drive letter 포함).
func fileURIToPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("parse uri %q: %w", uri, err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("unsupported scheme %q (file only)", u.Scheme)
	}
	p := u.Path
	// Windows: /C:/path → C:/path.
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p), nil
}

// segmentArchiveURI는 backend root URI + segmentNumber → segment archive URI.
//
// key 형식: seg-NNNNNN.tar.gz (zero-padded 6). 본 함수는 default key naming과 일치
// (rotation.defaultArchiveKey가 <tenant>/seg-%06d.tar.gz를 만들고, backend root는
// 이미 <tenant>까지 가리킴 — chain mode에서 backend는 tenant 하위 dir 가정).
func segmentArchiveURI(backendRoot string, segmentNumber int64) (string, error) {
	u, err := url.Parse(backendRoot)
	if err != nil {
		return "", fmt.Errorf("parse backend %q: %w", backendRoot, err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("unsupported scheme %q (file only)", u.Scheme)
	}
	// trailing slash 정규화.
	path := strings.TrimRight(u.Path, "/")
	return fmt.Sprintf("file://%s/seg-%06d.tar.gz", path, segmentNumber), nil
}

// emitRotationOutput은 --format에 따라 stdout에 결과를 씁니다 (single archive).
func emitRotationOutput(format string, out rotationOutput) {
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	fmt.Printf("RESULT          %s\n", out.Result)
	fmt.Printf("archiveUri      %s\n", out.ArchiveURI)
	fmt.Printf("archiveSha256   %s\n", out.ArchiveSHA256)
	fmt.Printf("segmentHash     %s\n", out.SegmentHash)
	if out.PrevSegmentHash != "" {
		fmt.Printf("prevSegmentHash %s\n", out.PrevSegmentHash)
	}
	fmt.Printf("entryCount      %d\n", out.EntryCount)
	fmt.Printf("manifestVersion %s\n", out.ManifestVersion)
	if out.CosignBundlePath != "" {
		fmt.Printf("cosignBundle    %s\n", out.CosignBundlePath)
	}
	if out.Reason != "" {
		fmt.Printf("reason          %s\n", out.Reason)
	}
	fmt.Println()
	fmt.Println("STEPS:")
	stepNames := make([]string, 0, len(out.Steps))
	for _, s := range out.Steps {
		mark := "FAIL"
		if s.OK {
			mark = "PASS"
		}
		fmt.Printf("  %-14s %s  %s\n", s.Name, mark, s.Detail)
		stepNames = append(stepNames, s.Name)
	}
	// sort for deterministic test display (informational).
	sort.Strings(stepNames)
	if out.OK {
		fmt.Println("\nPASS — segment archive verification successful.")
	} else {
		fmt.Println("\nFAIL — segment archive verification failed.")
	}
}

// emitChainOutput은 chain batch verify 결과를 stdout에 씁니다.
func emitChainOutput(format string, out chainOutput) {
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	fmt.Printf("CHAIN RESULT %s\n", out.Result)
	fmt.Printf("backend      %s\n", out.Backend)
	fmt.Printf("from         %d\n", out.FromSegment)
	fmt.Printf("to           %d\n", out.ToSegment)
	fmt.Printf("verified     %d\n", out.Verified)
	if out.Reason != "" {
		fmt.Printf("reason       %s\n", out.Reason)
		fmt.Printf("firstFailure seg=%d\n", out.FirstFailure)
	}
	fmt.Println()
	fmt.Println("SEGMENTS:")
	for _, s := range out.Segments {
		mark := "FAIL"
		if s.OK {
			mark = "PASS"
		}
		fmt.Printf("  seg=%-5d %s  %s\n", s.SegmentNumber, mark, s.ArchiveURI)
	}
	if out.OK {
		fmt.Printf("\nPASS — chain of %d segments verified.\n", out.Verified)
	} else {
		fmt.Printf("\nFAIL — chain broken at segment %d.\n", out.FirstFailure)
	}
}

// rotationUsage는 rotation 서브커맨드 사용법을 출력합니다.
func rotationUsage() {
	fmt.Fprintln(os.Stderr, `rosshield-audit-verify rotation — audit rotation segment archive 검증

사용법:
  rosshield-audit-verify rotation \
      --archive-uri file:///path/to/seg-000005.tar.gz \
      [--expected-sha256 <hex64>] \
      [--expected-segment-hash <hex64>] \
      [--prev-segment-hash <hex64>] \
      [--format table|json]

  rosshield-audit-verify rotation chain \
      --backend file:///path/to/audit-archives/<tenant>/ \
      --from-segment <N> [--to-segment <M>] \
      [--format table|json]

옵션 (single):
  --archive-uri            검증할 archive URI (file://...) — 필수
  --expected-sha256        archive 본문 sha256 hex (외부 채널의 expected; 옵션)
  --expected-segment-hash  manifest segment_hash hex (옵션)
  --prev-segment-hash      manifest prev_segment_hash hex (옵션)
  --cosign-bundle          cosign signature bundle 파일 (옵션; identity와 함께 활성화)
  --cosign-identity        cosign certificate-identity (예: ci@example.com)
  --cosign-oidc-issuer     cosign certificate-oidc-issuer (예: https://accounts.google.com)
  --cosign-binary          cosign binary 경로 (default: PATH의 cosign)
  --cosign-rekor-url       cosign --rekor-url (default: Sigstore public)

옵션 (chain):
  --backend                backend root URI (file:///path/to/tenant/) — 필수
  --from-segment           검증 시작 segmentNumber (기본 1)
  --to-segment             검증 종료 segmentNumber (기본 from 만)
  --cosign-bundle-dir      chain mode bundle 디렉터리 (각 segment seg-NNNNNN.cosign.bundle)
  --cosign-identity        cosign certificate-identity (모든 segment 공통)
  --cosign-oidc-issuer     cosign certificate-oidc-issuer
  --cosign-binary          cosign binary 경로
  --cosign-rekor-url       cosign --rekor-url`)
}

// ifExpected는 details 출력 보조 (expected가 빈 문자열이면 suffix 안 붙임).
func ifExpected(expected, suffix string) string {
	if expected == "" {
		return ""
	}
	return suffix
}
