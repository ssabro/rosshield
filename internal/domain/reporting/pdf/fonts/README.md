# NanumGothic 폰트 자산

E8 PDF 리포트의 한글 렌더링용 임베드 폰트.

## 파일

| 파일 | 출처 | sha256 (lowercase hex) |
|---|---|---|
| `NanumGothic.ttf` | https://github.com/google/fonts/raw/main/ofl/nanumgothic/NanumGothic-Regular.ttf | `76f45ef4a6bcff344c837c95a7dcc26e017e38b5846d5ae0cdcb5b86be2e2d31` |
| `LICENSE.txt` | https://github.com/google/fonts/raw/main/ofl/nanumgothic/OFL.txt | (SIL Open Font License 1.1) |

## 라이선스

NanumGothic은 NHN Corporation이 제작·배포하는 **SIL Open Font License 1.1** 폰트입니다.
재배포 가능, 단 다음을 준수해야 합니다.

1. 폰트는 단독으로 판매할 수 없습니다(다른 소프트웨어에 번들링은 가능).
2. 본 라이선스 텍스트(`LICENSE.txt`)를 함께 동봉해야 합니다(이 디렉터리에 동봉됨).
3. 원본 폰트의 Reserved Font Name("Nanum", "NanumGothic" 등)은 수정·파생 폰트의 이름으로 사용할 수 없습니다.

본 리포에서는 폰트 파일을 변경하지 않고 그대로 임베드합니다.

## 갱신 절차

1. 새 버전 다운로드: `curl -sL -o NanumGothic.ttf <상기 URL>`.
2. `sha256sum` 으로 hex 출력 확인.
3. 본 README의 sha256 행 수정.
4. `internal/domain/reporting/pdf/embed.go` 의 `//go:embed`는 변경 불필요(파일명 동일).
5. 골든 fixture sha256(`testdata/golden_*.sha256`) 재생성 — 폰트 byte가 변하면 PDF 출력 byte가 변합니다(R10-5 결정성 회귀 가드 작동).

## 왜 NanumGothic인가

E8 라이브러리 평가(`docs/design/notes/e8-pdf-library-eval.md` §4.2) 결과:

- **SIL OFL-1.1** — 재배포 가능한 오픈 폰트 라이선스. Open-core 코어(Apache-2.0)에 안전.
- 한글 글리프 완비, 영문·숫자 글리프도 자체 보유 → 한 폰트로 영문·한글 혼합 줄 가능.
- TTF 단일 파일(~2 MB) — `embed.FS`로 단일 바이너리 원칙(P3 에어갭) 유지.
- subset prefix가 단순 family name 그대로 출력되는 gopdf 구현 특성과 결합해 결정성 친화적
  (`signintech/gopdf` `strhelper.go:CreateEmbeddedFontSubsetName` 참조).
