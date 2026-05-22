# testdata — audit export bundle fixtures

본 디렉터리는 Phase 10.D-5 + 11.C-5 `rosshield-audit-verify export` 외부 검증 단위
테스트용 fixture 를 보존합니다.

| 파일 | 형식 | 설명 |
|---|---|---|
| `v1_bundle.ndjson.gz` | v1 (legacy, ~v0.9.0) | `_bundleVersion` 부재. 모든 entry 가 epoch=1 default. 3 entries. |
| `v2_bundle.ndjson.gz` | v2 (Phase 10.D-5, v0.10.0+) | `_bundleVersion="v2"` + `_chainKeyEpochs[]` 3 epoch + rotation entry 2건. 5 entries. |
| `v3_bundle.ndjson.gz` | v3 (Phase 11.C-4, v0.13.0+) | `_bundleVersion="v3"` + `_chainKeyEpochs[]` 2 epoch + `_hashVersionTransitionAt=3` + transition entry (seq=3) + key_rotated entry (seq=4) + v3 hash entries (seq=4,5). 5 entries 혼합. |

fixture 는 `fixture_gen_test.go` (TestGenerateFixtures, `-tags=fixturegen`) 가 deterministic
seed (chacha8) 로 재생성합니다. byte-identical 보존을 위해 ed25519 key 생성에 deterministic
PRNG 를 주입합니다.

재생성 절차:

```bash
go test -count=1 -tags=fixturegen -run TestGenerateFixtures ./cmd/rosshield-audit-verify/...
```

run 후 `v1_bundle.ndjson.gz` / `v2_bundle.ndjson.gz` / `v3_bundle.ndjson.gz` 가 갱신됩니다.
