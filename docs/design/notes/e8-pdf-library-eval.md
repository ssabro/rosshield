# E8 PDF Library Evaluation — Phase 1 리포트 생성기

Phase 1 E8 "서명 PDF + 외부 검증 성공" exit 기준을 만족할 Go PDF 생성 라이브러리를 평가한 노트. 결정성(byte-for-byte reproducible), 한글 임베드 폰트, Apache-2.0/MIT/BSD 라이선스, pure Go, 단일 바이너리 친화성을 우선한다.

스코프 가정:
- 입력: ScanSession 1건 (3 robot × CIS 팩 → ~30~100 페이지)
- 콘텐츠: 메타 헤더 / pass·fail·error counts / check별 상세(rationale, fixGuidance, evidence sha256 참조) / audit chain anchor / Ed25519 서명 트레일러
- 운영 환경: Windows·Linux·macOS, 에어갭 1급
- 라이선스 거부 목록: AGPL, GPL, EULA proprietary, 워터마크 강제

---

## 1. Top 3 후보

### 1.1 signintech/gopdf — **권장 본선 1순위**

- 라이선스 **MIT**, 100% pure Go, 외부 의존 없음 (zero CGO). 활발히 maintenance(2026-04 기준 정기 commit).
- `AddTTFFont` + Unicode subfont embedding으로 **한글(NanumGothic·NotoSansKR) 직접 임베드**. CJK 공식 지원 명시.
- API가 절차적이지만 **PDF object 트리에 가까운 저수준 제어** 가능 — Info dict / Catalog 추가 메타키 박기 용이(`info *PdfInfo` 필드 노출).
- `Now()` 같은 비결정성 소스를 호출하므로, 결정성 확보를 위해 **CreationDate를 명시적으로 SOURCE_DATE_EPOCH 기반으로 강제**하는 wrapper가 필요(2.3 참조).
- 표·다단 레이아웃은 직접 그려야 하지만, CIS 체크리스트는 단순 표 + 페이지네이션이라 충분.

### 1.2 phpdave11/gofpdf (fork) — **본선 2순위 / fallback**

- 라이선스 **MIT**, pure Go, Go stdlib만 의존. 원본(jung-kurt)이 **2021-11-13 archived**된 후 가장 활성 fork. UTF-8 TTF 전용으로 정리되어 한글·일본어·아랍어 OK.
- `SetCreationDate()` + `SetCatalogSort()`를 **공식적으로 결정성 비교용으로 노출** — 우리 use case와 정확히 일치.
- API 표현력은 1.1 gopdf와 동급. 다만 maintenance 활성도는 가변적이므로 **vendoring + 자체 Patch 정책**을 채택해야 안전(`SESSION_HANDOFF.md` 결정 로그 후보).
- maroto v2가 여전히 jung-kurt/gofpdf를 의존하므로, fork로 교체할 때 import path 비호환을 유발할 수 있음 → **maroto와는 양자택일**.

### 1.3 johnfercher/maroto v2 — **본선 3순위 (생산성 카드)**

- 라이선스 **MIT**, v2.4.0 (2026-03-15), gofpdf 위에 **Bootstrap 12-grid wrapper**. row/col 기반 선언적 API → 30~100p 표 리포트가 매우 빨리 작성됨.
- Custom UTF-8 폰트 예제 공식 제공(한글 OK). `ExecutionTime` 같은 메트릭 필드를 PDF에 자동 박는 기능이 있어 **결정성을 깰 수 있음** — 옵션으로 끄거나 감싸야 한다.
- 의존성: 내부적으로 jung-kurt/gofpdf(아카이브됨)를 끌고 들어옴 → Phase 1 채택 시 **archived 의존성을 우리가 transitive로 떠안는다**는 라이선스·공급망 리스크.
- 추천 포지션: **Phase 1 채택 보류, Phase 2 UI 개선 시점에 재평가**. Phase 1은 "서명 + 결정성" 증명이 우선.

---

## 2. 상세 비교 표

| 기준 | gopdf (signintech) | gofpdf (phpdave11 fork) | maroto v2 | pdfcpu | unipdf | gofpdf (jung-kurt 원본) |
|---|---|---|---|---|---|---|
| **라이선스** | MIT ✅ | MIT ✅ | MIT ✅ | Apache-2.0 ✅ | **AGPL or 상용** ❌ | MIT (archived) ⚠ |
| **Pure Go (CGO X)** | ✅ | ✅ | ✅ (gofpdf 의존) | ✅ | ✅ | ✅ |
| **외부 binary 의존** | 없음 | 없음 | 없음 | 없음 | 없음 (단 license server 호출 가능) | 없음 |
| **활성 maintenance** | ✅ 활발 | 🟡 중간 (fork) | ✅ 활발 (v2.4 2026-03) | ✅ 활발 | ✅ (상용) | ❌ 2021-11 archived |
| **한글/CJK TTF 임베드** | ✅ 공식 (Unicode subfont) | ✅ UTF-8 TTF 전용 | ✅ 예제 제공 | ⚠ 생성 API 빈약 | ✅ | ✅ |
| **결정성 친화 API** | 🟡 wrapper 필요 | ✅ `SetCreationDate`+`SetCatalogSort` 명시 | 🟡 ExecutionTime 자동필드 차단 필요 | ⚠ 생성 시나리오 제한 | ✅ (상용) | ✅ |
| **표·레이아웃 API** | 🟡 저수준 (직접 그리기) | 🟡 저수준 | ✅ 12-grid 선언적 | ⚠ JSON 템플릿 | ✅ 풍부 | 🟡 저수준 |
| **Custom Info dict / Catalog 메타** | ✅ `PdfInfo` 노출 | ✅ `SetInfo`+`SetCatalogSort` | 🟡 wrapper 우회 필요 | ✅ 풍부 | ✅ | ✅ |
| **저수준 byte 제어** | ✅ object writer 접근 가능 | ✅ | ❌ wrapper 너무 두꺼움 | ✅ 본업 | ✅ | ✅ |
| **바이너리 크기 영향** | ~1.5 MB | ~1.5 MB | ~2 MB (gofpdf 포함) | ~3 MB (큼) | ~5 MB (큼) | ~1.5 MB |
| **GitHub stars (참고)** | 2.9k | (fork) | 2.7k | 6k+ | 1.7k | 5k+ (archived) |
| **CIS 30~100p 리포트 적합성** | ✅ | ✅ | ✅ (가장 빠름) | ❌ | ✅ | ⚠ 신규 채택 비추천 |

**탈락 사유 정리**:
- **unipdf** — AGPL 또는 상용 라이선스 강제, 무라이선스 시 워터마크. open-core(코어 Apache-2.0) 정책과 양립 불가. 즉시 탈락.
- **pdfcpu** — Apache-2.0 + pure Go로 라이선스·결정성은 최상이지만 본질이 **PDF 가공·검증 도구**. 신규 다단·텍스트 리포트 생성 API가 빈약(`Create` 명령은 JSON 템플릿 기반, dynamic content에 부적합). **단, E8 PDF의 검증·재해석·압축·암호화 보조 도구로는 1순위 유틸리티 라이브러리**로 채택 권장.
- **chromedp / wkhtmltopdf** — 외부 binary(Chromium·wkhtml) 강제 의존, 단일 바이너리 원칙(P7)과 에어갭 빌드(P3) 위반. 기각.
- **jung-kurt/gofpdf 원본** — 2021-11-13 archived. 신규 채택 금지. fork(phpdave11) 또는 의존 wrapper(maroto)가 transitive로 끌어오는 경우만 허용.

---

## 3. 결정 권장값

### R10-1 (선정): **`github.com/signintech/gopdf` 단독 채택**

**이유 (3줄)**:
1. **라이선스·공급망 안전** — MIT + pure Go + 외부 의존 0. archived 의존성을 끌어오지 않음(maroto·gofpdf-fork 대비 우위).
2. **CJK 1급 시민** — 한국어 폰트 임베드가 공식 지원 항목(NanumGothic TTF subfont 포함). E8가 한국어 설계 컨텍스트를 PDF로 직접 출력해야 하므로 결정적.
3. **저수준 제어 + 결정성 가능** — Info dict / Catalog 직접 접근 가능, byte writer를 wrapping해서 비결정적 timestamp·map 순회를 격리할 여지가 있음. maroto 같은 두꺼운 wrapper는 동일 작업이 어렵다.

**보조**:
- `github.com/pdfcpu/pdfcpu` 를 **검증·후처리 유틸리티**로 함께 채택. 생성한 PDF의 구조 검증, 페이지 수 확인, 압축, 메타데이터 추출에 사용. CLI도 함께 제공 가능.
- maroto v2는 **Phase 2 후반(UI 풍부화 단계)에서 재평가**. Phase 1은 "서명·결정성 증명" 우선이므로 wrapper 두께를 늘리지 않는다.

**비채택 결정 이유 — phpdave11/gofpdf**: 결정성 API가 명시적이라 매력적이지만, fork 거버넌스가 단일 메인테이너에 고정되어 장기 공급망 리스크가 gopdf보다 크다. 선택 보류, 2순위 fallback 유지.

---

## 4. 함정·테스트 케이스 (필수 체크리스트)

### 4.1 결정성(reproducible) 함정

PDF 생성기가 byte-identical 출력을 내려면 아래 모두를 잠가야 한다. **하나라도 새면 audit chain anchor가 깨진다.**

| 함정 | 영향 | 차단 방법 |
|---|---|---|
| `time.Now()` 호출 (CreationDate, ModDate) | 매 실행마다 PDF 헤더 변경 | 빌드 시점에 `SOURCE_DATE_EPOCH` 또는 `ScanSession.CompletedAt` UTC를 주입 → `SetInfo(Created/Modified)`로 고정 |
| Go map 순회 비결정성 | Info dict, 폰트 리소스 순서가 매번 다름 | dict serialize 전 `sort.Strings(keys)` 강제. gopdf 내부에서 map을 쓰는 지점은 PR 또는 wrapper로 우회 |
| 자동 생성 image/object ID | 같은 이미지여도 ID가 다르면 xref·offset 변동 | 이미지 등록 순서를 sha256(content) 기준으로 정렬. evidence 참조도 마찬가지 |
| 동시성(goroutine) 작업 | rendering 순서 비결정 | 단일 goroutine에서 PDF write. 데이터 수집은 병렬 OK, 생성 단계는 직렬 |
| Float 포맷 차이 | `%.6g` 등에서 플랫폼 의존 | gopdf 내부도 float을 fixed precision으로 출력하는지 확인 — 깨지면 wrapper에서 감싸야 함 |
| TTF 임베드 시 폰트 subset 차이 | 같은 글자라도 subset 알고리즘이 비결정적이면 폰트 stream byte 변동 | 폰트 전체 임베드(subset 끄기) 또는 subset 입력을 sorted unique rune set으로 고정 |
| zlib/Deflate 압축 레벨 | Go 표준 zlib는 결정적이지만 레벨 변경 시 byte 변동 | `flate.BestCompression` 등 명시 고정. 라이브러리 default가 바뀔 가능성 차단 |
| 빌드 경로 / `runtime.GOOS` 분기 | 디버그·메타 필드에 OS 이름 박힐 위험 | PDF Info dict의 Creator·Producer 문자열을 **고정 상수**로 (예: `rosshield/0.1`) |

### 4.2 한글 폰트 임베드 함정

- **NanumGothic / NotoSansKR Regular** — Apache-2.0 / SIL OFL 1.1 라이선스이므로 재배포 OK. 라이선스 텍스트를 `assets/fonts/LICENSE.NanumGothic.txt` 등으로 동봉.
- TTF 파일을 **embed.FS**로 바이너리에 박아 단일 바이너리 원칙 유지. 외부 폰트 디렉터리 검색 금지.
- Subset 사용 시 **첫 페이지에 한글이 없고 30페이지에 한글이 처음 등장**하는 케이스에서 한글 글리프가 누락되는 버그 가능 — 미리 전체 contents에서 사용 rune set을 모은 뒤 폰트 등록.
- 영문·한글 혼합 줄에서 baseline·line-height 차이 → CIS 체크 ID(영문)와 한국어 설명이 한 줄에 섞일 때 줄 간격 깨짐. **테스트 케이스: "R8-1 한글 설명 영문ID 혼합 줄"** 회귀 테스트로 박을 것.
- Bold·Italic 변형이 필요하면 별도 TTF 등록 (NanumGothicBold). Synthetic bold(글자 두 번 그리기)는 결정성·렌더링 모두 위험.

### 4.3 페이지 길이 변화 시 layout shift

- check 개수가 늘면 페이지 수가 변하고, 페이지 번호·목차·xref가 모두 변함. **결정성은 동일 입력에서만 성립**한다는 점을 문서·테스트에서 명시.
- ScanSession 결과가 동일해도 **CIS 팩 버전 업그레이드** 시 출력이 변함 → PDF의 Info dict에 `BenchmarkPackVersion`을 박아 외부 검증자가 입력을 식별할 수 있게 한다.
- 표가 페이지 경계에 걸칠 때 row split 처리 — gopdf 저수준 API에서 직접 측정 후 줄 바꿈해야 함. 회귀 테스트: **체크 1개 / 50개 / 200개 / 1000개** 각각 byte-identical 확인.

### 4.4 결정성 회귀 테스트 (E8 필수)

```go
func TestReportDeterminism(t *testing.T) {
    fixture := loadGoldenSession(t, "testdata/session-3robots-cis.json")
    pdf1, _ := report.Build(fixture, BuildOptions{Now: fixedTime})
    pdf2, _ := report.Build(fixture, BuildOptions{Now: fixedTime})
    require.Equal(t, sha256.Sum256(pdf1), sha256.Sum256(pdf2))
}
```

- `BuildOptions.Now`는 명시적으로 주입(injection) — 라이브러리가 직접 `time.Now()` 부르면 안 된다.
- Golden PDF를 `testdata/golden/`에 commit하고 CI에서 byte-diff 확인. 변경 시 의도 명시 후 재생성.

---

## 5. PDF 서명 형식 권장 (R10-2)

### 5.1 옵션 비교

| 옵션 | 설명 | 장점 | 단점 |
|---|---|---|---|
| **A. PAdES (CMS in /Contents)** | ETSI EN 319 142, PDF 표준. CMS 서명을 PDF Contents 필드에 임베드, /ByteRange로 서명 영역 제외 | Acrobat 등 표준 뷰어가 즉시 검증 가능. eIDAS 호환. 자체완결 (PDF 1개로 전송) | CMS·X.509 인증서 체인 필요 → Ed25519 raw key와 호환 안 됨 (X.509 Ed25519는 RFC 8410 필요). PDF byte 조작·ByteRange 계산 복잡. 결정성 보장이 더 까다로움 |
| **B. PDF Info dict에 X-Signature-* 키** | Info dict에 `X-Signature-KeyId`·`X-Signature-Base64` 커스텀 키 박기 | PDF 1개로 전송. 구현 단순 | **닭과 달걀 문제** — Info dict이 PDF 본문에 포함되므로 서명 계산 시점에 Info dict이 비어 있어야 하고, 서명 후 Info dict을 채우면 다시 hash가 변함. ByteRange 트릭으로 우회 가능하지만 사실상 옵션 A의 simplified 변형이 됨. 표준 뷰어가 인식 못 함 |
| **C. Detached `.sig` 파일** | `report.pdf` + `report.pdf.sig` (Ed25519 raw signature 64 byte 또는 minisign 포맷) 함께 배포 | 가장 단순. PDF는 손대지 않음 → 결정성과 직교. `crypto/ed25519`만 있으면 검증. 외부 검증자(감사인)가 `openssl`/`minisign`/직접 Go로 검증 가능. 키 회전·재서명도 PDF 변경 없이 가능 | 파일 2개 전송 필요 (zip 또는 디렉터리로 묶음). 표준 PDF 뷰어의 "서명됨" UI 표시는 없음 |

### 5.2 권장: **Phase 1 = 옵션 C (detached `.sig`) + minisign 호환 포맷**

**이유**:
1. **결정성 직교** — PDF 생성과 서명이 분리되어 4.1의 함정이 서명 계산에 전염되지 않는다. PDF 생성 → sha256 → Ed25519 서명 → `.sig` 출력. 단순 명료.
2. **Ed25519 raw key 그대로** — X.509·CMS·인증서 체인 인프라 없이 Ed25519 keypair만으로 동작. Phase 1 키 관리(`KMS·HSM 보류`)와 정합. eIDAS·X.509 도입은 Phase 3에서 PAdES로 업그레이드 가능.
3. **외부 검증의 단순성** — 감사인이 받는 검증 절차:
   ```bash
   # rosshield-verify CLI (Phase 1 E8에서 함께 제공)
   rosshield verify report.pdf --public-key auditor-issued-pubkey.ed25519
   # 또는 minisign 호환 모드로 표준 도구 사용
   minisign -V -p pubkey -m report.pdf
   ```
   외부 검증이 핵심 exit 기준이므로, **검증자가 추가 라이브러리 설치 없이 minisign 1개로 가능**한 것이 가장 강한 보장.
4. **PDF Info dict에는 메타만** — `BenchmarkPackVersion`, `ScanSessionID`, `AuditChainAnchorSHA256`, `Producer=rosshield/<ver>` 같은 **검증에 필요한 컨텍스트**만 박는다. 서명 자체는 박지 않는다 (서명 계산 ↔ Info dict 순환 회피).

### 5.3 파일 레이아웃 (Phase 1 기본 산출물)

```
exports/<sessionID>/
├── report.pdf                      # 결정론적 PDF (gopdf)
├── report.pdf.sig                  # Ed25519 detached signature (minisign 포맷)
├── report.pdf.sha256               # 평문 sha256 (검증 보조)
├── audit-chain.json                # 해시 체인 발췌 (anchor 포함)
└── verify.md                       # 검증 절차 한국어/영어
```

서명 입력:
```
sha256(report.pdf) || ASCII("rosshield-pdf-v1") || scanSessionID
```
prefix tag로 cross-protocol 공격 차단. minisign trusted comment에 `scanSessionID·tenantID·packVersion`을 박아 사람이 읽을 수 있게 한다.

### 5.4 Phase 3 업그레이드 경로 (참고)

- 감사인 표준 검증을 더 강하게 요구하는 고객(금융·공공)이 나오면 **PAdES B-LT**로 업그레이드. `digitorus/pdfsign`(pure Go, AES/QES 지원)이 후보.
- 그때까지는 옵션 C가 결정성·외부 검증·키 관리 단순성에서 모두 우월.

---

## 6. 다음 액션 (E8 Stage 분해 힌트)

1. **Stage A** — gopdf wrapper(`internal/report/pdf`) 골격 + 결정성 회귀 테스트 1건(빈 페이지 + 한글 1줄). NanumGothic embed.FS 임베드. 라이선스 텍스트 동봉.
2. **Stage B** — ScanSession → 표 렌더링. CIS 체크 1개 / 50개 / 1000개 fixture로 byte-identical 확인.
3. **Stage C** — Ed25519 서명 모듈(`internal/report/sign`) + `.sig` 출력. minisign 호환 포맷 확정. CLI `rosshield verify`.
4. **Stage D** — pdfcpu로 후처리 검증(페이지 수 sanity, Info dict 필수 키 존재). audit chain anchor 박기.
5. **Stage E** — 감사인 검증 시나리오 e2e 테스트 (서명 PDF → tampered byte 1개 변경 → 검증 실패 확인).

각 Stage는 trunk-based로 main에 직접 commit, golden PDF는 `testdata/golden/` 에 저장.

---

## 참고 출처

- gopdf — https://github.com/signintech/gopdf (MIT, Unicode CJK subfont embedding 공식)
- gofpdf (jung-kurt 원본) — archived 2021-11-13, https://github.com/jung-kurt/gofpdf
- gofpdf fork (phpdave11) — https://github.com/phpdave11/gofpdf (MIT, UTF-8 only)
- maroto v2 — https://github.com/johnfercher/maroto (MIT, v2.4.0 2026-03-15)
- pdfcpu — https://github.com/pdfcpu/pdfcpu (Apache-2.0, 검증·후처리 유틸로 채택)
- unipdf — https://github.com/unidoc/unipdf (AGPL/상용, **탈락**)
- digitorus/pdfsign — https://github.com/digitorus/pdfsign (Phase 3 PAdES 후보)
- PAdES 표준 — ETSI EN 319 142, https://en.wikipedia.org/wiki/PAdES
- minisign — https://jedisct1.github.io/minisign/ (옵션 C 검증 도구)
- Reproducible PDF / SOURCE_DATE_EPOCH — https://reproducible-builds.org/docs/timestamps/
- Go crypto/ed25519 — https://pkg.go.dev/crypto/ed25519
