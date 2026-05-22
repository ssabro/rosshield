// export_verify.go — Phase 10.D-5 + 11.C-5: audit entries export bundle 외부 검증 서브커맨드.
//
// 본 파일은 `rosshield-audit-verify export` 서브커맨드를 구현합니다. 외부 감사인이
// rosshield-server 의 `Repo.Export` / `ExportV2` / `ExportV3` 가 만든 NDJSON+gzip 번들의
// 무결성을 단독 검증할 수 있게 합니다.
//
// 세 가지 bundle wire 형식 (자동 판별 — `_bundleVersion` 필드 유무·값으로 분기):
//
//	v1 (legacy, ~v0.9.0): _bundleVersion 부재 → 모든 entry 를 epoch=1 default 로 처리.
//	    fg-verify 는 signature line 의 단일 _publicKey 로 _signedDigest 를 검증.
//	    hash chain 은 모두 v1 (canonicalMetaJSONv1, 7 키).
//
//	v2 (Phase 10.D-5, v0.10.0+): _bundleVersion = "v2" + _chainKeyEpochs[] 포함.
//	    각 entry 의 keyEpoch 필드 + signature line 의 _keyId 가 _chainKeyEpochs 와
//	    cross-reference 됩니다. epoch transition 정합 (audit.chain.key_rotated entry)
//	    도 검증. hash chain 은 모두 v1 (wire format 만 v2, hash 함수는 v1).
//
//	v3 (Phase 11.C-4, v0.13.0+): _bundleVersion = "v3" + _chainKeyEpochs[] + 추가 보장:
//	    - signature line `_hashVersionTransitionAt` 가 transition entry seq 노출 (포함된 경우).
//	    - 각 entry line 이 LeaderEpoch (nil 이면 omit) 노출.
//	    - hash chain 은 seq <= transitionSeq → v1 hash, 그 외 → v3 hash (9 키, keyEpoch +
//	      leaderEpoch 알파벳순 추가).
//	    - transition entry 자체는 v1 hash 로 계산됨 (sqliterepo.Repo.Append 분기 일관).
//
// 사용법:
//
//	rosshield-audit-verify export \
//	    --bundle file:///path/to/audit-export.ndjson.gz \
//	    [--format table|json]
//
// 검증 단계:
//  1. fetch  — file:// 본문 fetch + sha256 계산.
//  2. gunzip — gzip 풀어 NDJSON bytes 획득.
//  3. parse  — entry line + signature line 분리.
//  4. signature — signature line 의 _publicKey 또는 _chainKeyEpochs 에서 keyId 매칭
//     public key 로 _signedDigest 를 Ed25519.Verify.
//  5. digestRecompute — entries 본문 stream 의 sha256 == _signedDigest.
//  6. chain — entry n 의 prev_hash == entry n-1 의 hash + ComputeEntryHash 재계산.
//     v3 bundle 은 hashVersionTransitionAt 기준 v1/v3 hash 함수 분기.
//  7. epochTransition — audit.chain.key_rotated entry 의 keyEpoch 증가 + 두 epoch
//     모두 _chainKeyEpochs 에 존재 (v2/v3).
//  8. hashVersionTransition — _hashVersionTransitionAt 가 bundle 범위 안의 transition
//     entry seq 와 일치 (v3 만, transition entry 가 bundle 안에 포함된 경우).

package main

import (
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/ssabro/rosshield/internal/domain/audit"
)

// exportOutput 은 export 서브커맨드 결과 와이어 형식 (table·JSON 모두 동일).
type exportOutput struct {
	OK                      bool         `json:"ok"`
	Result                  string       `json:"result"` // "PASS" | "FAIL"
	Reason                  string       `json:"reason,omitempty"`
	BundlePath              string       `json:"bundlePath"`
	BundleSHA256            string       `json:"bundleSha256"`
	BundleVersion           string       `json:"bundleVersion"` // "v1" | "v2" | "v3"
	EntryCount              int          `json:"entryCount"`
	EpochCount              int          `json:"epochCount"`
	SigningKeyID            string       `json:"signingKeyId"`
	FromSeq                 int64        `json:"fromSeq"`
	ToSeq                   int64        `json:"toSeq"`
	RotationEntries         int          `json:"rotationEntries"`
	HashVersionTransitionAt int64        `json:"hashVersionTransitionAt,omitempty"` // v3 transition entry seq (0 = 부재/미포함)
	Steps                   []stepResult `json:"steps"`
}

// runExport 는 `export` 서브커맨드 진입.
func runExport(args []string) int {
	fs := flag.NewFlagSet("rosshield-audit-verify export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bundlePath := fs.String("bundle", "", "audit export bundle (.ndjson.gz) 경로 — 필수")
	format := fs.String("format", "table", "table | json")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "rosshield-audit-verify: export flag parse: %v\n", err)
		exportUsage()
		return 2
	}
	if *bundlePath == "" {
		fmt.Fprintln(os.Stderr, "rosshield-audit-verify: export --bundle <path> required")
		exportUsage()
		return 2
	}
	if *format != "table" && *format != "json" {
		fmt.Fprintf(os.Stderr, "rosshield-audit-verify: unknown --format %q\n", *format)
		return 2
	}
	out := verifyExportBundle(*bundlePath)
	emitExportOutput(*format, out)
	if !out.OK {
		return 1
	}
	return 0
}

// verifyExportBundle 은 한 export bundle 의 모든 검증 단계를 수행해 결과를 반환합니다.
//
// 본 함수는 stdlib + crypto/ed25519 + JSON parsing + 도메인 ComputeEntryHash 만 사용.
// 다른 외부 의존 0 — 외부 감사인이 단독 빌드 가능 (P5 일관).
func verifyExportBundle(bundlePath string) exportOutput {
	out := exportOutput{BundlePath: bundlePath}

	body, err := os.ReadFile(bundlePath)
	if err != nil {
		return exportFail(out, "fetch", fmt.Sprintf("read bundle: %v", err))
	}
	sum := sha256.Sum256(body)
	out.BundleSHA256 = hex.EncodeToString(sum[:])
	out.Steps = append(out.Steps, stepResult{Name: "fetch", OK: true,
		Detail: fmt.Sprintf("%d bytes", len(body))})

	ndjson, err := gunzipExport(body)
	if err != nil {
		return exportFail(out, "gunzip", err.Error())
	}
	out.Steps = append(out.Steps, stepResult{Name: "gunzip", OK: true,
		Detail: fmt.Sprintf("%d ndjson bytes", len(ndjson))})

	entriesBytes, sigLine, err := splitExportLines(ndjson)
	if err != nil {
		return exportFail(out, "parse", err.Error())
	}

	var sig audit.ExportSignatureLine
	if err := json.Unmarshal(sigLine, &sig); err != nil {
		return exportFail(out, "parse", fmt.Sprintf("signature line decode: %v", err))
	}
	out.BundleVersion = bundleVersionLabel(sig.BundleVersion)
	out.SigningKeyID = sig.KeyID
	out.FromSeq = sig.From
	out.ToSeq = sig.To
	out.EpochCount = len(sig.ChainKeyEpochs)
	out.HashVersionTransitionAt = sig.HashVersionTransitionAt
	out.Steps = append(out.Steps, stepResult{Name: "parse", OK: true,
		Detail: fmt.Sprintf("bundleVersion=%s keyId=%s from=%d to=%d epochs=%d transitionAt=%d",
			out.BundleVersion, sig.KeyID, sig.From, sig.To, len(sig.ChainKeyEpochs),
			sig.HashVersionTransitionAt)})

	// chainKeyEpochs map 구성. v1 (또는 _bundleVersion 부재) 는 signature line 의
	// 단일 publicKey 를 epoch=1 default 로 처리 — 모든 entry 가 epoch=1 가정.
	epochMap, err := buildEpochMap(sig)
	if err != nil {
		return exportFail(out, "epochs", err.Error())
	}

	// digest recompute.
	digest := sha256.Sum256(entriesBytes)
	digestHex := hex.EncodeToString(digest[:])
	if !strings.EqualFold(digestHex, sig.SignedDigest) {
		return exportFail(out, "digestRecompute",
			fmt.Sprintf("recomputed=%s signature=%s", digestHex, sig.SignedDigest))
	}
	out.Steps = append(out.Steps, stepResult{Name: "digestRecompute", OK: true,
		Detail: "sha256(entries) == _signedDigest"})

	// signature verify — _keyId 로 epochMap 에서 public key lookup.
	pub, err := lookupSigningPublicKey(sig, epochMap)
	if err != nil {
		return exportFail(out, "signature", err.Error())
	}
	sigBytes, err := hex.DecodeString(sig.Signature)
	if err != nil {
		return exportFail(out, "signature", fmt.Sprintf("decode hex: %v", err))
	}
	if len(sigBytes) != ed25519.SignatureSize {
		return exportFail(out, "signature",
			fmt.Sprintf("signature size=%d want=%d", len(sigBytes), ed25519.SignatureSize))
	}
	if !ed25519.Verify(pub, digest[:], sigBytes) {
		return exportFail(out, "signature", "ed25519 verify failed")
	}
	out.Steps = append(out.Steps, stepResult{Name: "signature", OK: true,
		Detail: fmt.Sprintf("ed25519.Verify OK (key=%s)", sig.KeyID)})

	// chain — entries 파싱 + hash 재계산. v3 bundle 은 hashVersionTransitionAt 기준 v1/v3 분기.
	entries, err := parseExportEntries(entriesBytes)
	if err != nil {
		return exportFail(out, "chain", err.Error())
	}
	out.EntryCount = len(entries)
	if err := verifyHashChainForBundle(entries, sig); err != nil {
		return exportFail(out, "chain", err.Error())
	}
	chainDetail := fmt.Sprintf("%d entries hash-linked", len(entries))
	if sig.BundleVersion == audit.BundleVersionV3 && sig.HashVersionTransitionAt > 0 {
		chainDetail += fmt.Sprintf(" (v1/v3 split at seq=%d)", sig.HashVersionTransitionAt)
	}
	out.Steps = append(out.Steps, stepResult{Name: "chain", OK: true, Detail: chainDetail})

	// epoch transition — v2 + v3. audit.chain.key_rotated entry 의 keyEpoch 검증.
	if sig.BundleVersion == audit.BundleVersionV2 || sig.BundleVersion == audit.BundleVersionV3 {
		rotations, err := verifyEpochTransitions(entries, epochMap)
		if err != nil {
			return exportFail(out, "epochTransition", err.Error())
		}
		out.RotationEntries = rotations
		out.Steps = append(out.Steps, stepResult{Name: "epochTransition", OK: true,
			Detail: fmt.Sprintf("%d rotation entries verified", rotations)})
	} else {
		out.Steps = append(out.Steps, stepResult{Name: "epochTransition", OK: true,
			Detail: "skipped (v1 bundle — single epoch default)"})
	}

	// hashVersionTransition — v3 만. _hashVersionTransitionAt 가 bundle 안의 transition
	// entry seq 와 일치하는지 검증 (transition entry 가 bundle 범위 안에 있는 경우).
	if sig.BundleVersion == audit.BundleVersionV3 {
		if err := verifyHashVersionTransitionMarker(entries, sig); err != nil {
			return exportFail(out, "hashVersionTransition", err.Error())
		}
		detail := fmt.Sprintf("transitionAt=%d", sig.HashVersionTransitionAt)
		if sig.HashVersionTransitionAt == 0 {
			detail = "no transition entry in bundle range"
		}
		out.Steps = append(out.Steps, stepResult{Name: "hashVersionTransition", OK: true,
			Detail: detail})
	} else {
		out.Steps = append(out.Steps, stepResult{Name: "hashVersionTransition", OK: true,
			Detail: "skipped (v1/v2 bundle — v1 hash function only)"})
	}

	out.OK = true
	out.Result = "PASS"
	return out
}

// exportFail 은 stepName 단계 실패 시 reason 을 채워 exportOutput 을 마감합니다.
func exportFail(out exportOutput, stepName, reason string) exportOutput {
	out.OK = false
	out.Result = "FAIL"
	out.Reason = reason
	out.Steps = append(out.Steps, stepResult{Name: stepName, OK: false, Detail: reason})
	return out
}

// bundleVersionLabel 은 wire 의 _bundleVersion (빈 문자열이면 v1) 을 사람-읽기용 라벨로 변환.
func bundleVersionLabel(wire string) string {
	if wire == "" {
		return "v1"
	}
	return wire
}

// gunzipExport 는 bundle bytes 를 gunzip 하여 NDJSON bytes 를 반환합니다.
//
// 한도 (256 MiB) 는 reporting.extractBundleTarGz 와 동일 정책 — DoS 회피.
func gunzipExport(body []byte) ([]byte, error) {
	const maxNDJSON = 256 * 1024 * 1024
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gzip open: %w", err)
	}
	defer func() { _ = gz.Close() }()
	buf := bytes.NewBuffer(make([]byte, 0, len(body)*2))
	n, err := io.CopyN(buf, gz, maxNDJSON+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("gzip read: %w", err)
	}
	if n > maxNDJSON {
		return nil, fmt.Errorf("ndjson exceeds %d bytes", maxNDJSON)
	}
	return buf.Bytes(), nil
}

// splitExportLines 는 NDJSON bytes 를 (entries-stream, signatureLine) 으로 분리합니다.
//
// signature line 은 NDJSON 의 마지막 non-empty line 으로 `_` prefix 키 (예: "_keyId")
// 가 있는 라인입니다 — entry line 과 underscore prefix 로 구분.
//
// entries-stream 은 모든 entry line + 개행 — sha256 재계산 input 과 일치.
func splitExportLines(ndjson []byte) (entries, sigLine []byte, err error) {
	trimmed := bytes.TrimRight(ndjson, "\n")
	if len(trimmed) == 0 {
		return nil, nil, errors.New("empty ndjson")
	}
	// 마지막 라인 찾기.
	idx := bytes.LastIndexByte(trimmed, '\n')
	if idx < 0 {
		// 단일 라인 — entry 0 + signature only.
		if !looksLikeSignatureLine(trimmed) {
			return nil, nil, errors.New("ndjson has no signature line (underscore prefix)")
		}
		return nil, trimmed, nil
	}
	sigCandidate := trimmed[idx+1:]
	if !looksLikeSignatureLine(sigCandidate) {
		return nil, nil, errors.New("last ndjson line is not a signature line (underscore prefix)")
	}
	// entries stream 은 마지막 줄(서명) 이전까지의 모든 line + 마지막 개행 포함.
	// repo.go Export 가 SignedDigest 입력으로 사용한 byte stream 과 byte-identical.
	entries = ndjson[:idx+1]
	return entries, sigCandidate, nil
}

// looksLikeSignatureLine 은 ndjson 라인이 underscore prefix 키 로 시작하는 signature line 인지 확인.
//
// JSON object 의 첫 토큰이 `{"_` 인 경우만 true.
func looksLikeSignatureLine(line []byte) bool {
	trimmed := bytes.TrimLeft(line, " \t")
	if len(trimmed) < 3 {
		return false
	}
	return trimmed[0] == '{' && trimmed[1] == '"' && trimmed[2] == '_'
}

// buildEpochMap 은 signature line 으로부터 epoch → public key map 을 구성합니다.
//
// v2/v3: _chainKeyEpochs[] 모든 epoch 가 map 에 들어감.
// v1: signature line 의 단일 _publicKey 를 epoch=1 로 default 처리 — 모든 entry 가
//
//	epoch=1 으로 가정 (v0.9.0 이하 호환).
func buildEpochMap(sig audit.ExportSignatureLine) (map[int64]ed25519.PublicKey, error) {
	out := map[int64]ed25519.PublicKey{}
	if sig.BundleVersion != audit.BundleVersionV2 && sig.BundleVersion != audit.BundleVersionV3 {
		// v1 — single epoch default.
		pub, err := decodeEd25519PublicHex(sig.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("v1 publicKey: %w", err)
		}
		out[1] = pub
		return out, nil
	}
	if len(sig.ChainKeyEpochs) == 0 {
		return nil, fmt.Errorf("%s bundle has empty _chainKeyEpochs", sig.BundleVersion)
	}
	for _, ce := range sig.ChainKeyEpochs {
		if ce.Epoch <= 0 {
			return nil, fmt.Errorf("%s chainKeyEpochs: invalid epoch %d", sig.BundleVersion, ce.Epoch)
		}
		if _, dup := out[ce.Epoch]; dup {
			return nil, fmt.Errorf("%s chainKeyEpochs: duplicate epoch %d", sig.BundleVersion, ce.Epoch)
		}
		pub, err := decodeEd25519PublicHex(ce.PublicKeyHex)
		if err != nil {
			return nil, fmt.Errorf("%s epoch=%d publicKey: %w", sig.BundleVersion, ce.Epoch, err)
		}
		out[ce.Epoch] = pub
	}
	return out, nil
}

// decodeEd25519PublicHex 는 hex 문자열을 ed25519.PublicKey 로 디코드합니다.
//
// 크기 검증: 32 bytes. 다른 크기는 invalid.
func decodeEd25519PublicHex(s string) (ed25519.PublicKey, error) {
	raw, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("hex decode: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("publicKey size=%d want=%d", len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// lookupSigningPublicKey 는 signature line 의 _keyId 와 매칭되는 epoch 의 public key 를 반환.
//
// v2/v3: _chainKeyEpochs 안에 keyId 가 같은 row 가 정확히 1개 있어야 함. 없으면 error.
// v1: signature line 의 _publicKey 를 직접 사용 (epoch=1 default).
func lookupSigningPublicKey(sig audit.ExportSignatureLine, epochMap map[int64]ed25519.PublicKey) (ed25519.PublicKey, error) {
	if sig.BundleVersion != audit.BundleVersionV2 && sig.BundleVersion != audit.BundleVersionV3 {
		if pub, ok := epochMap[1]; ok {
			return pub, nil
		}
		return nil, errors.New("v1 bundle: epoch=1 missing in map")
	}
	for _, ce := range sig.ChainKeyEpochs {
		if ce.KeyID == sig.KeyID {
			if pub, ok := epochMap[ce.Epoch]; ok {
				return pub, nil
			}
			return nil, fmt.Errorf("%s: keyId=%s epoch=%d in chainKeyEpochs but missing in map",
				sig.BundleVersion, sig.KeyID, ce.Epoch)
		}
	}
	return nil, fmt.Errorf("%s: signing keyId=%s not found in _chainKeyEpochs",
		sig.BundleVersion, sig.KeyID)
}

// parseExportEntries 는 entries stream (NDJSON bytes) 를 audit.Entry slice 로 파싱.
func parseExportEntries(entriesBytes []byte) ([]audit.Entry, error) {
	if len(entriesBytes) == 0 {
		return nil, nil
	}
	var out []audit.Entry
	for i, line := range bytes.Split(bytes.TrimRight(entriesBytes, "\n"), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		e, err := audit.UnmarshalEntryLine(line)
		if err != nil {
			return nil, fmt.Errorf("entry line %d: %w", i+1, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// verifyHashChainForBundle 은 bundle version 에 따라 적절한 hash 함수로 chain 을 검증합니다.
//
// v1/v2 bundle: 모든 entry 가 v1 hash (canonicalMetaJSONv1, 7 키).
// v3 bundle: seq <= _hashVersionTransitionAt 인 entry 는 v1 hash, 그 외 entry 는 v3 hash
//
//	(canonicalMetaJSONv3, 9 키 — keyEpoch + leaderEpoch 알파벳순 추가).
//
// transition entry (seq == transitionSeq) 자체는 v1 hash 로 계산됨 — sqliterepo.Repo.Append
// 가 entry.Seq > transitionSeq 조건으로 분기하므로 transition entry 는 분기 직전. v3 bundle
// 인데 _hashVersionTransitionAt == 0 인 경우 (bundle 범위 밖) — 모든 entry seq 와 비교 시
// 모두 transitionSeq 보다 큼 → 모두 v3 hash 로 검증. 단, v3 hash 함수는 KeyEpoch/LeaderEpoch
// 가 nil 이면 omit 하므로 v1 entry 에 대해서도 v1 hash 와 byte-identical (backward compat 안전망).
func verifyHashChainForBundle(entries []audit.Entry, sig audit.ExportSignatureLine) error {
	useV3 := sig.BundleVersion == audit.BundleVersionV3
	transitionSeq := sig.HashVersionTransitionAt
	for i, e := range entries {
		hashFn := selectHashFunc(useV3, transitionSeq, e.Seq)
		recomputed, err := hashFn(e.PrevHash, e.PayloadDigest, e)
		if err != nil {
			return fmt.Errorf("entry seq=%d compute hash: %w", e.Seq, err)
		}
		if recomputed != e.Hash {
			return fmt.Errorf("entry seq=%d hash mismatch: recomputed=%x stored=%x",
				e.Seq, recomputed[:8], e.Hash[:8])
		}
		if i == 0 {
			continue
		}
		if entries[i-1].Hash != e.PrevHash {
			return fmt.Errorf("entry seq=%d prevHash mismatch with seq=%d hash",
				e.Seq, entries[i-1].Seq)
		}
	}
	return nil
}

// hashFunc 는 audit.ComputeEntryHash / ComputeEntryHashV3 공통 signature.
type hashFunc func(prevHash, payloadDigest audit.Hash, e audit.Entry) (audit.Hash, error)

// selectHashFunc 는 v3 bundle 일 때 transitionSeq 와 entry.Seq 를 비교해 v1/v3 hash 함수를
// 선택합니다. v1/v2 bundle 은 항상 v1.
//
// 분기 기준 (sqliterepo.Repo recomputeHashForSeq 와 일관):
//
//	useV3 == false                                    → v1.
//	transitionSeq == 0                                → v1 (transition 미발생 또는 bundle 밖).
//	useV3 && transitionSeq > 0 && entry.Seq > transitionSeq → v3.
//	그 외                                              → v1.
func selectHashFunc(useV3 bool, transitionSeq, entrySeq int64) hashFunc {
	if useV3 && transitionSeq > 0 && entrySeq > transitionSeq {
		return audit.ComputeEntryHashV3
	}
	return audit.ComputeEntryHash
}

// verifyHashVersionTransitionMarker 는 v3 bundle 의 _hashVersionTransitionAt 이 bundle 안
// transition entry seq 와 일치하는지 검증합니다.
//
// 검증 규칙:
//   - _hashVersionTransitionAt == 0: bundle 범위 안에 transition entry 가 없어야 함. 있으면 FAIL.
//   - _hashVersionTransitionAt > 0: bundle 범위 안에 action=audit.chain.hash_version_changed
//     entry 가 정확히 1개 + 그 seq == _hashVersionTransitionAt + 활성 분기 경계와 일관.
//
// 추가로 transitionSeq 이후 모든 entry 가 v3 hash domain 임을 확인하기 위해 keyEpoch nil
// 검증은 하지 않음 (HA 미활성 환경에서도 v3 hash 함수는 nil omit 으로 backward compat 보장).
func verifyHashVersionTransitionMarker(entries []audit.Entry, sig audit.ExportSignatureLine) error {
	const transitionAction = audit.ActionHashVersionChanged
	var (
		found    []int64
		foundSeq int64
	)
	for _, e := range entries {
		if e.Action == transitionAction {
			found = append(found, e.Seq)
			foundSeq = e.Seq
		}
	}
	switch {
	case sig.HashVersionTransitionAt == 0 && len(found) > 0:
		return fmt.Errorf("hashVersionTransitionAt=0 but bundle contains transition entry at seq=%d",
			found[0])
	case sig.HashVersionTransitionAt > 0 && len(found) == 0:
		return fmt.Errorf("hashVersionTransitionAt=%d but no transition entry in bundle",
			sig.HashVersionTransitionAt)
	case sig.HashVersionTransitionAt > 0 && len(found) > 1:
		return fmt.Errorf("hashVersionTransitionAt=%d but bundle contains %d transition entries",
			sig.HashVersionTransitionAt, len(found))
	case sig.HashVersionTransitionAt > 0 && foundSeq != sig.HashVersionTransitionAt:
		return fmt.Errorf("hashVersionTransitionAt=%d does not match transition entry seq=%d",
			sig.HashVersionTransitionAt, foundSeq)
	}
	return nil
}

// verifyEpochTransitions 는 v2 bundle 의 audit.chain.key_rotated entry 가 정합 epoch
// 전환을 표현하는지 검증합니다.
//
// 검증 사항:
//   - rotation entry 의 keyEpoch 가 chainKeyEpochs 에 존재해야 함.
//   - rotation entry 직전 entry 의 keyEpoch 가 chainKeyEpochs 에 존재해야 함.
//   - 두 epoch 모두 chainKeyEpochs 에 존재.
//   - rotation entry 의 keyEpoch > 이전 entry 의 keyEpoch (epoch 단조 증가).
//
// rotation entry 수를 반환.
func verifyEpochTransitions(entries []audit.Entry, epochMap map[int64]ed25519.PublicKey) (int, error) {
	const rotationAction = "audit.chain.key_rotated"
	rotations := 0
	for i, e := range entries {
		if e.Action != rotationAction {
			continue
		}
		if e.KeyEpoch == nil {
			return 0, fmt.Errorf("rotation entry seq=%d missing keyEpoch", e.Seq)
		}
		if _, ok := epochMap[*e.KeyEpoch]; !ok {
			return 0, fmt.Errorf("rotation entry seq=%d epoch=%d not in chainKeyEpochs",
				e.Seq, *e.KeyEpoch)
		}
		if i == 0 {
			// 첫 entry 가 rotation 인 경우 — 이전 entry 없음. epoch 단순 존재 확인으로 종료.
			rotations++
			continue
		}
		prev := entries[i-1]
		if prev.KeyEpoch == nil {
			return 0, fmt.Errorf("rotation entry seq=%d: prev entry seq=%d missing keyEpoch",
				e.Seq, prev.Seq)
		}
		if _, ok := epochMap[*prev.KeyEpoch]; !ok {
			return 0, fmt.Errorf("rotation entry seq=%d: prev epoch=%d not in chainKeyEpochs",
				e.Seq, *prev.KeyEpoch)
		}
		if *e.KeyEpoch <= *prev.KeyEpoch {
			return 0, fmt.Errorf("rotation entry seq=%d: epoch %d must exceed prev epoch %d",
				e.Seq, *e.KeyEpoch, *prev.KeyEpoch)
		}
		rotations++
	}
	return rotations, nil
}

// emitExportOutput 은 --format 에 따라 stdout 에 결과를 씁니다.
func emitExportOutput(format string, out exportOutput) {
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	fmt.Printf("RESULT                  %s\n", out.Result)
	fmt.Printf("bundle                  %s\n", out.BundlePath)
	fmt.Printf("bundleSha256            %s\n", out.BundleSHA256)
	fmt.Printf("bundleVersion           %s\n", out.BundleVersion)
	fmt.Printf("entryCount              %d\n", out.EntryCount)
	fmt.Printf("epochCount              %d\n", out.EpochCount)
	fmt.Printf("signingKeyId            %s\n", out.SigningKeyID)
	fmt.Printf("fromSeq                 %d\n", out.FromSeq)
	fmt.Printf("toSeq                   %d\n", out.ToSeq)
	fmt.Printf("rotationEntries         %d\n", out.RotationEntries)
	fmt.Printf("hashVersionTransitionAt %d\n", out.HashVersionTransitionAt)
	if out.Reason != "" {
		fmt.Printf("reason                  %s\n", out.Reason)
	}
	fmt.Println()
	fmt.Println("STEPS:")
	stepNames := make([]string, 0, len(out.Steps))
	for _, s := range out.Steps {
		mark := "FAIL"
		if s.OK {
			mark = "PASS"
		}
		fmt.Printf("  %-18s %s  %s\n", s.Name, mark, s.Detail)
		stepNames = append(stepNames, s.Name)
	}
	sort.Strings(stepNames) // informational — step order display deterministic.
	if out.OK {
		fmt.Println("\nPASS — audit export bundle verification successful.")
	} else {
		fmt.Println("\nFAIL — audit export bundle verification failed.")
	}
}

// exportUsage 는 export 서브커맨드 사용법을 출력합니다.
func exportUsage() {
	fmt.Fprintln(os.Stderr, `rosshield-audit-verify export — audit entries export bundle 검증

사용법:
  rosshield-audit-verify export \
      --bundle <path/to/audit-export.ndjson.gz> \
      [--format table|json]

옵션:
  --bundle  audit export bundle (.ndjson.gz) 경로 — 필수
  --format  출력 포맷 (table | json, 기본 table)

bundle 호환:
  v1 (~v0.9.0)   — _bundleVersion 부재. 모든 entry 가 epoch=1 default + v1 hash.
  v2 (v0.10.0+)  — _bundleVersion="v2" + _chainKeyEpochs[] 포함. epoch 별 public key
                    cross-reference + rotation entry transition 정합 검증. hash 함수는
                    여전히 v1.
  v3 (v0.13.0+)  — _bundleVersion="v3" + _chainKeyEpochs[] + _hashVersionTransitionAt.
                    transition entry 이전 = v1 hash, 이후 = v3 hash (9 키 — keyEpoch +
                    leaderEpoch 알파벳순 추가). entry-level LeaderEpoch 노출.

exit code:
  0  PASS — 모든 단계 통과
  1  FAIL — 검증 실패
  2  ARG  — invalid CLI args`)
}
