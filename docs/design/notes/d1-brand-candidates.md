# D1 — 제품 브랜드 후보 어휘 풀

> 코드 네임스페이스 `rosshield`는 확정(2026-04-23, D2와 함께). 이 문서는 **사용자 대면 제품 브랜드** 확정을 위한 후보 어휘 풀과 1차 상표/도메인 점검 결과. **최종 결정: Lodestar** (2026-05-18, D-P7-1).
> 작성일: 2026-05-11. 상태: **D1 보류 중 (출원 잠금 D8-4 해제 후 확정 예정)**.

---

## 0. TL;DR

- 12개 후보 어휘를 brainstorming하고 WebSearch 기반 1차 상표 충돌 점검 수행.
- **Top 3 추천**: `Custos` (★★★★) / `Lodestar` (★★★★) / `Praxis` (★★★) — 단, **모두 변리사 정밀 검색 후 최종 결정 필요**.
- 5개 후보(Aegis, Sentinel, Helix, Vector, Axiom)는 **상표 강충돌**로 사실상 폐기 권장.
- KIPRIS·USPTO TESS·EUIPO eSearch 정밀 결과는 **본 문서 범위 밖** — WebSearch는 1차 지표일 뿐.

---

## 1. 작명 원칙

본 절은 후보 평가의 기준이며, "FleetGuard"가 왜 깨졌는지(2026-04-23 폐기) 학습한 결과를 반영한다.

### 1.1 발음·기억성

- 영어권·한국어권 화자 양쪽이 **1회 청취로 정확히 받아쓸 수 있어야** 한다.
- 음절 2~3개 권장. 4음절 이상은 줄임말로 변형됨(예: "Cybersecurity Platform" → "사이플" 같은 비공식 약칭이 생기면 검색 분리).
- 한국어 표기 시 외래어 표기법으로 1가지로 수렴해야 한다(예: "Aegis"는 "이지스"로 거의 고정 — 좋음 / "Praxis"는 "프락시스/프랙시스" 분기 — 약점).

### 1.2 상표 충돌 회피 (가장 중요)

- 점검 대상 관할: **USPTO(미국)**, **KIPO/KIPRIS(한국)**, **EUIPO(EU)**, **WIPO Madrid**(국제).
- 점검 클래스: 본 제품은 **NICE Class 9**(컴퓨터 소프트웨어, 다운로드 가능 SW), **Class 42**(SaaS, 보안 컨설팅, 인증·감사 서비스)가 핵심. Class 35(B2B 컨설팅), Class 41(교육·트레이닝)도 부가 점검.
- **동일 클래스 내 동일·유사 표장은 출원 거절 사유**. 다른 클래스라도 저명상표는 광범위 보호(예: "Apple"은 IT 전반).
- 폐기 사례 학습: **"FleetGuard"** = Cummins(자동차 부품, 광범위 등록) + Attestor.ai(인포섹) — 단어 자체가 **"fleet" + "guard" 양쪽 다 흔한 보안 어휘**여서 충돌 다발. 합성어는 두 단어 모두 검색해야 함.

### 1.3 도메인 가용성

- 우선순위: `.com` >= `.io` > `.dev` > `.ai` > `.security` > 기타.
- **`.com` 미확보는 critical 약점** — enterprise 구매자가 검색 시 첫 매칭이 경쟁사·이종 회사로 가면 신뢰 손실.
- `.io`는 IT 제품 표준이지만 영국령 인도양 지정 폐지 진행 중(2024~) — 장기 의존 위험. 백업 도메인 필수.
- `.security`, `.audit` 등 신규 TLD는 SEO 약하고 인지도 낮음. 보조용으로만.

### 1.4 검색 친화

- Google·GitHub·Stack Overflow에서 **제품명 단독 검색 시 1페이지 점유**가 목표.
- 흔한 단어(예: "Vector", "Anchor")는 SEO 약점. 디버깅 시에도 노이즈가 많아 사용자 불편.
- 약어·이니셜리즘(예: "RAMP")은 정부 프로그램(TX-RAMP, FedRAMP)과 충돌 위험 — 주의.

### 1.5 보안·로봇 맥락 적합성

- 의미가 **연상되되 직설적이지 않은 것**이 enterprise B2B에 적합.
  - 너무 직설: "RobotSecure", "FleetAudit" — 차별화 없음·검색 약함·상표 약함.
  - 적절: "Aegis"(방패), "Custos"(수호자), "Lodestar"(길잡이 별) — 은유적.
  - 너무 추상: "Aether", "Helix" — 의미 연결 약함.

### 1.6 enterprise 구매자 신뢰감

- **장난스러운 이름 금지**: 동물 이름(Rabbit, Fox), 만화 캐릭터, 인터넷 밈 — B2B에선 평가 페널티.
- **단호하고 권위적인 어조**: 라틴·그리스어 어원, 천문·항법 메타포, 단음절 명사가 강함.
- 발화 시 자신감을 잃지 않는 단어 선택 — 영업·데모 시 매번 발음하므로.

---

## 2. 후보 어휘 풀

각 후보는 `WebSearch`로 1차 충돌 점검 완료. **WebSearch는 정밀 검색이 아니므로**, 결과는 모두 "표면적 신호"로만 해석. 변리사 의뢰 전 참고용.

---

### 후보 1: Aegis

**의미·어원**: 그리스어 αἰγίς. 제우스/아테나의 방패. 영어에서 "보호·후원"의 은유로 정착.
**발음**: 영어 [ˈiːdʒɪs] / 한국어 "이지스" (외래어 표기 거의 고정)
**도메인 가용성**: `aegis.com`·`aegis.io`·`aegis.dev`·`aegis.ai` 모두 **점유**. `aegis-ot.com`, `aegiscds.com` 등 보안 분야 다수 사용.
**상표 검색 결과**:
- USPTO: **다수 충돌** — Aegis Cyber Defense Systems(Aegis Defender Pro), AEGIS CyberOps, Aegis Intelligence, ÆGIS-OT 등 사이버보안 분야 동시 사용.
- KIPO: 별도 검증 필요. 한국에서도 "이지스" 자산운용·보안 제품 다수.
- 발견된 충돌: **Class 9·42 강충돌** (cybersecurity 카테고리에 5개 이상 활성 사용처).
**보안·로봇 contextual fit**: 5/5 (방패 메타포 완벽)
**기억성·검색 친화도**: 2/5 (검색 시 노이즈 다수)
**리스크**: 신규 진입 시 SEO에서 기존 Aegis 보안 회사들에 묻힐 가능성 높음. 등록 거절 가능성 매우 높음.
**추천도**: ★ (사실상 폐기 권장)

---

### 후보 2: Sentinel

**의미·어원**: 라틴어 sentinella. 보초·감시병. 보안 분야의 archetype 단어.
**발음**: 영어 [ˈsɛntɪnəl] / 한국어 "센티넬"
**도메인 가용성**: `sentinel.com` 점유(SentinelOne 등). `sentinelone.com`이 산업 표준.
**상표 검색 결과**:
- USPTO: **압도적 강충돌** — SentinelOne(NYSE 상장 2021, $1.2B IPO, EDR 마켓 리더, Gartner Magic Quadrant 5년 연속 Leader). Microsoft Sentinel(SIEM)도 별개로 존재.
- KIPO: 한국 시장에서도 SentinelOne 인지도 높음.
- 발견된 충돌: **Class 9·42 압도적 충돌**, EDR/XDR 카테고리 사실상 동의어화.
**보안·로봇 contextual fit**: 5/5
**기억성·검색 친화도**: 1/5 (검색 결과 99%가 SentinelOne)
**리스크**: 사용 시 **법적 분쟁 직행 + 시장 인식 혼동**. 절대 사용 불가.
**추천도**: ★ (사용 금지)

---

### 후보 3: Custos

**의미·어원**: 라틴어 custos = 수호자·관리자. 도서관 사서, 야간 경비, 후견인 등 권위적 의미. 학술·법률 어휘로 살아 있음.
**발음**: 영어 [ˈkʌstoʊs] / 한국어 "쿠스토스" (외래어 표기 1방향 수렴)
**도메인 가용성**: `custos.com`·`custos.io` 점유(보안·미디어 회사 다수). `custos.dev`·`custos.ai` 미확인 — 변리사·도메인 broker 확인 필요.
**상표 검색 결과**:
- USPTO: 약~중 충돌 — Custos Media Technologies(forensic watermarking), Custos IQ(offensive cybersecurity, AI 기반), CustOS Engineering(보안 컨설팅), Custos Security LLC, Apache Airavata Custos(오픈소스 sci-gateway 미들웨어), Cubro Custos(network monitoring). **모두 niche 또는 소규모** — Sentinel·Aegis 수준은 아님.
- KIPO: 별도 검증 필요. 한국 등록 사례 인지 못함(WebSearch 한계).
- 발견된 충돌: **Class 9·42에 niche 사용처 다수, 그러나 dominant player 없음**. Custos IQ가 가장 강력 — 도메인·시장 위치 모니터링 필요.
**보안·로봇 contextual fit**: 4/5 (수호자 의미 직접적이지만 너무 흔하지 않음)
**기억성·검색 친화도**: 4/5 (라틴어 차용으로 권위감, 노이즈 적음)
**리스크**: 합성형(예: `CustosROS`, `Custos Audit`)으로 차별화하면 등록 가능성 ↑. 단독 "Custos"는 niche 충돌로 거절 risk 중간.
**추천도**: ★★★★ (Top 3 후보)

---

### 후보 4: Vigil

**의미·어원**: 라틴어 vigilia = 깨어있음. 야간 감시·기도·경계.
**발음**: 영어 [ˈvɪdʒɪl] / 한국어 "비질" (한국어로 어색, "비길/뷔질" 분기 가능)
**도메인 가용성**: `vigil.com` 점유. `vigil.io`·`vigil.dev`·`vigil.ai` 미확인.
**상표 검색 결과**:
- USPTO: VIGIL™ (USPTO 98049026, Vigil Inc, security token hardware 카테고리), VIGILANT (Vigilant Inc, Class 42 SW services, 2025 출원). **유사 표장 다수**.
- KIPO: 별도 검증 필요.
- 발견된 충돌: Class 9 직접 충돌(security token), Class 42 유사 표장 진행 중.
**보안·로봇 contextual fit**: 4/5
**기억성·검색 친화도**: 3/5 (한국어 표기 분기가 약점)
**리스크**: 짧은 단어라 유사 표장 대비 협소한 보호 범위. VIGILANT 등록 진행 시 충돌 가능.
**추천도**: ★★ (보류)

---

### 후보 5: Praxis

**의미·어원**: 그리스어 πρᾶξις = 실천·실무. 이론(theoria)과 대비. 학술·교육·전문직 어휘.
**발음**: 영어 [ˈpræksɪs] / 한국어 "프락시스/프랙시스" (분기)
**도메인 가용성**: `praxis.com` 점유. `praxis.security` 점유 여부 미확인.
**상표 검색 결과**:
- USPTO: 보안 분야에 다수 — Praxis Data Security(GaaS), Praxis Computing(LA 보안 컨설팅), Praxis Security Labs(Kai Roer, behavior monitoring), Cyber Praxis(EU). **단, 모두 컨설팅·서비스 중심, 제품 SW 단독 dominant 없음**.
- KIPO: 한국에 "프락시스" 자산 운용·교육·의료 분야 다수 — 보안 SW 등록은 미확인.
- 발견된 충돌: Class 42 서비스 강충돌, **Class 9 다운로드 SW는 상대적으로 약함**.
**보안·로봇 contextual fit**: 3/5 ("실천"의 은유 — 컴플라이언스·감사 의미와 연결 가능하나 직접적이지 않음)
**기억성·검색 친화도**: 3/5 (한국어 표기 분기 약점, 영문 검색은 의외로 noise 적음)
**리스크**: Class 42 충돌로 단독 사용 risk. **합성어**(예: `Praxis Audit`, `PraxisROS`)로 차별화 시 등록 가능성 ↑.
**추천도**: ★★★ (Top 3 후보, 단 합성형 권장)

---

### 후보 6: Helix

**의미·어원**: 그리스어 ἕλιξ = 나선. DNA·생명·구조 메타포.
**발음**: 영어 [ˈhiːlɪks] / 한국어 "헬릭스"
**도메인 가용성**: `helix.com` 점유(다수). 보안 분야 도메인은 모두 점유.
**상표 검색 결과**:
- USPTO: **강충돌** — FireEye Helix™(security operations platform, 등록 트레이드마크). FireEye는 Trellix로 합병됐으나 Helix 브랜드는 잔존. Class 9·42 정확히 동일 카테고리.
- KIPO: 한국에서도 Helix 표장 다수.
- 발견된 충돌: **Class 9·42 직접 충돌**, dominant player 명확.
**보안·로봇 contextual fit**: 2/5 (보안과 연결 약함)
**기억성·검색 친화도**: 2/5 (Helix는 게놈·과학·암호화폐 등 광범위 사용)
**리스크**: FireEye/Trellix가 보호 적극 행사 가능성. 등록 거절 매우 유력.
**추천도**: ★ (사실상 폐기)

---

### 후보 7: Kepler

**의미·어원**: 천문학자 Johannes Kepler. 행성 궤도·항법·정밀성 메타포.
**발음**: 영어 [ˈkɛplər] / 한국어 "케플러"
**도메인 가용성**: `kepler.com` 점유. `kepler.io`·`kepler.dev` 미확인.
**상표 검색 결과**:
- USPTO: KEPLER (Spruce Systems Inc, 다운로드 가능 OS SW for personal data storage — Class 9 직접 충돌). Kepler Computing Inc 다수 특허.
- KIPO: 별도 검증 필요.
- 발견된 충돌: **Class 9 직접 충돌** (Spruce Systems의 KEPLER 등록).
**보안·로봇 contextual fit**: 2/5 (보안과 연결 약함, 항법은 robot에 약하게 연결)
**기억성·검색 친화도**: 3/5 (천문학자 이름 노이즈 다수)
**리스크**: Class 9 등록 거절 가능성 높음.
**추천도**: ★★ (보류)

---

### 후보 8: Anchor

**의미·어원**: 영어 anchor = 닻. "신뢰의 기준점·고정"의 은유.
**발음**: 영어 [ˈæŋkər] / 한국어 "앵커"
**도메인 가용성**: `anchor.com` 점유. 모든 짧은 도메인 점유.
**상표 검색 결과**:
- USPTO: **다수 충돌** — Anchor Labs(ANCHORAGE® cryptocurrency custody, Class 36/42), Anchor Advanced Products, Anchor Claims Management 등.
- KIPO: 한국 "앵커" 다수 등록(미디어, 패션, 식품).
- 발견된 충돌: 보안 직접 충돌은 Anchor Labs(crypto custody)뿐이지만, 일반어 보호 어려움.
**보안·로봇 contextual fit**: 2/5 (트러스트 앵커 의미는 있으나 약함)
**기억성·검색 친화도**: 1/5 (영어 일반 단어, 노이즈 압도적)
**리스크**: 일반어로 distinctiveness 부족 — 등록 거절 가능성. SEO 약점 매우 큼.
**추천도**: ★ (폐기 권장)

---

### 후보 9: Vector

**의미·어원**: 라틴어 vector = 운반자. 수학·물리에서 방향·크기. 보안에선 "공격 벡터" 부정적 연상.
**발음**: 영어 [ˈvɛktər] / 한국어 "벡터"
**도메인 가용성**: `vector.com` 점유(자동차 SW). 모든 도메인 점유.
**상표 검색 결과**:
- USPTO: **압도적 충돌** — Vectra AI(NDR 마켓 리더, San Jose, 2011~, 113개국), VECTOROS, Vector Informatik 등.
- KIPO: 한국 다수.
- 발견된 충돌: **Class 9·42 직접 충돌**, 보안 분야 dominant player(Vectra AI).
**보안·로봇 contextual fit**: 2/5 ("공격 벡터" 부정 연상)
**기억성·검색 친화도**: 1/5
**리스크**: Vectra AI 측 보호 행사 가능성 높음. 시장 혼동.
**추천도**: ★ (폐기)

---

### 후보 10: Pivot

**의미·어원**: 영어 pivot = 회전축·전환점.
**발음**: 영어 [ˈpɪvət] / 한국어 "피벗"
**도메인 가용성**: `pivot.com` 점유. 짧은 도메인 모두 점유.
**상표 검색 결과**:
- USPTO: PIVOT (Pivot Software Solutions Inc 등록), 다수.
- KIPO: 한국에 "피벗" 다수(스타트업·교육).
- 발견된 충돌: Class 9 직접 충돌.
**보안·로봇 contextual fit**: 1/5 (보안과 연결 거의 없음)
**기억성·검색 친화도**: 2/5 (영어 일반 단어, 비즈니스 자기계발 jargon으로 소비됨)
**리스크**: 의미 fit 약하고 충돌 다수 — 사용 명분 부족.
**추천도**: ★ (폐기)

---

### 후보 11: Axiom

**의미·어원**: 그리스어 ἀξίωμα = 자명한 진리·공리. 수학·논리.
**발음**: 영어 [ˈæksiəm] / 한국어 "악시옴/액시엄" (분기)
**도메인 가용성**: `axiom.com` 점유. **`axiom.security` 활성 점유** (Privileged Access Management 솔루션, 2025 활성).
**상표 검색 결과**:
- USPTO: AXIOM (Design Eyewear Group, Class 9 등록 4460669), AXIOM LEGAL(Axiom Global Inc, Class 42), AXIOM SYSTEMS 등 다수.
- KIPO: 별도 검증 필요.
- 발견된 충돌: **`axiom.security` PAM 솔루션이 직접 경쟁 카테고리** — critical 충돌. Class 9·42 등록도 다수.
**보안·로봇 contextual fit**: 3/5 (논리·정확성 — 결정론적 증거와 연결 가능)
**기억성·검색 친화도**: 2/5 (한국어 표기 분기, axiom.security 직접 경쟁)
**리스크**: PAM 직접 경쟁자가 동일 도메인 사용 — 시장 혼동 critical.
**추천도**: ★ (폐기 권장)

---

### 후보 12: Lodestar

**의미·어원**: 고대 영어 lādestēorra = 길잡이 별(북극성). 항해·지표·궁극의 기준.
**발음**: 영어 [ˈloʊdˌstɑːr] / 한국어 "로드스타" (외래어 표기 1방향 수렴, 자동차 모델 연상 약점)
**도메인 가용성**: `lodestar.com` 점유. `lodestar.io`·`lodestar.dev`·`lodestar.ai` 일부 미확인 — 변리사·domain broker 확인 필요. `lodestar.security` 미확인.
**상표 검색 결과**:
- USPTO: LODESTAR Corporation(Class 42, **2010-07-31 Dead/Cancelled**), LODESTAR by Jesse Bohn(SaaS for fan conventions — 우리 카테고리와 무관), LODESTAR by Edenic Era(VR game SW — 무관), LNK9 by LodeStar Technology Inc.
- KIPO: 별도 검증 필요. 한국 자동차 "Lodestar" 모델(쌍용/KG) 인지도 — Class 12 충돌 있으나 Class 9·42는 무관.
- 발견된 충돌: **보안·SW 분야 dominant player 없음**. 기존 SW 등록(SaaS, VR game)은 우리 카테고리와 분명히 다름. 차량 모델(Class 12)은 Class 9·42 등록에 직접 장애 안 됨.
**보안·로봇 contextual fit**: 4/5 (북극성 = 신뢰의 기준점, 컴플라이언스·증거의 은유로 우수)
**기억성·검색 친화도**: 4/5 (의미 명확, 영문 SEO 노이즈 적음, 한국어 표기 안정)
**리스크**: 짧지 않은 음절(2음절+r)이지만 발음 장벽 낮음. 차량 모델 연상은 minor.
**추천도**: ★★★★ (Top 3 후보)

---

## 3. Top 3 추천

**Top 3 선정 기준**: 상표 충돌 risk가 niche·중간 수준 이하 + 보안/로봇 맥락 fit + enterprise 신뢰감 + 검색 친화도.

### 1순위: **Custos** ★★★★

- **강점**: 라틴어 어원의 권위, "수호자" 의미 직접 fit, 한국어 표기 안정("쿠스토스"), 영문 검색 노이즈 적음.
- **약점**: niche 보안 회사 5~6개 활성 사용 — 단독 단어 등록 risk 중간. **합성형 권장** (`Custos`만으로 부족하면 `Custos Audit`, `Custos OS`, `Custos for Robotics` 등).
- **트레이드오프**: 학술적 어조라 영업·데모 시 첫 인상 무겁다는 평이 가능. 그러나 enterprise B2B에 적합.
- **다음 액션**: 변리사에게 USPTO Class 9·42, KIPO Class 9·42, EUIPO 정밀 검색 의뢰. `custos.io`/`.ai`/`.dev` WHOIS 확인.

### 2순위: **Lodestar** ★★★★

- **강점**: 보안·SW 분야 dominant player 없음 — 등록 가능성 가장 높음. 의미("길잡이 별") = 신뢰의 기준점, 컴플라이언스 메시지와 매우 강하게 연결. 한국어 표기 안정.
- **약점**: 자동차 모델 연상 minor, 도메인 점유 상황 추가 확인 필요.
- **트레이드오프**: Custos보다 더 시적이라 marketing copy가 풍부해질 수 있음. 단, enterprise보다 brand-driven 인상이 강함.
- **다음 액션**: USPTO Class 9·42 정밀 + LODESTAR Corporation의 dead status 재확인. `lodestar.io`/`.dev`/`.ai` WHOIS.

### 3순위: **Praxis** ★★★

- **강점**: "실천" 의미가 감사·실무 컴플라이언스와 잘 연결. 영문 검색에서 도메인 분리 양호.
- **약점**: 한국어 표기 분기("프락시스/프랙시스"), Class 42 컨설팅 분야에 다수 활성 사용처 — 단독 단어 risk.
- **트레이드오프**: **합성형 강력 권장** — `Praxis Audit`, `PraxisROS` 형태가 등록 가능성 ↑. Custos·Lodestar보다 단독 등록 risk가 명확히 높음.
- **다음 액션**: 합성 후보 2~3개 확정 후 검색 재수행.

**3개 모두 공통 다음 액션**: 변리사 정밀 검색(USPTO TESS / KIPRIS / EUIPO eSearch / WIPO Madrid Monitor) 후 1개 확정.

---

## 4. 도메인 가용성 점검 결과 표

> WebSearch 기반 1차 점검. WHOIS 직접 확인은 차후 단계.

| 후보 | .com | .io | .dev | .ai | .security | KIPO 출원 가능성(추정) |
|---|---|---|---|---|---|---|
| Aegis | 점유(다수) | 점유 | 점유 | 점유 | 미확인 | 매우 낮음 (포화) |
| Sentinel | 점유(SentinelOne) | 점유 | 점유 | 점유 | 점유 | 거의 0 (저명상표) |
| Custos | 점유(보안·미디어) | 점유 | **미확인** | **미확인** | 미확인 | 중간 (niche 충돌) |
| Vigil | 점유 | 미확인 | 미확인 | 미확인 | 미확인 | 중간 |
| Praxis | 점유 | 미확인 | 미확인 | 미확인 | 미확인 | 중간 (Class 42 강) |
| Helix | 점유(다수) | 점유 | 점유 | 점유 | 미확인 | 매우 낮음 (FireEye) |
| Kepler | 점유 | 미확인 | 미확인 | 미확인 | 미확인 | 낮음 (Class 9 충돌) |
| Anchor | 점유(다수) | 점유 | 점유 | 점유 | 미확인 | 매우 낮음 (일반어) |
| Vector | 점유(Vector Informatik) | 점유 | 점유 | 점유 | 미확인 | 거의 0 (Vectra AI) |
| Pivot | 점유 | 점유 | 점유 | 점유 | 미확인 | 낮음 |
| Axiom | 점유 | 점유 | 점유 | 점유 | **점유(PAM 경쟁자)** | 낮음 |
| Lodestar | 점유 | **미확인** | **미확인** | **미확인** | 미확인 | 높음 (보안 dominant 없음) |

**해석**: `.com`은 모든 후보에서 점유 — 신생 진입 시 `.com` 확보는 사실상 불가능 또는 고비용 구매. `.io`/`.dev`/`.ai` 중 1개 확보가 현실적 목표.

**WebSearch 한계 명시**: 본 점검은 WHOIS 직접 조회가 아닌 검색 엔진 기반. **구매 가능 여부·가격은 Namecheap·Cloudflare Registrar에서 직접 확인 필수**.

---

## 5. 다음 단계

### 5.1 사용자 의사결정 필요 (즉시)

1. **Top 3 (Custos / Lodestar / Praxis) 중 1차 좁히기**: 1개 또는 2개로 압축 → 변리사 의뢰 비용 절감.
2. **합성형 vs 단독형**: `Lodestar` 단독 vs `LodestarROS`/`Lodestar Audit` 등 합성형 — 합성형이 등록 가능성 ↑이지만 brand 단순성 ↓.
3. **이번 사이클에서 D1 확정할지 vs 출원 잠금(D8-4) 해제 후로 연기할지**: 후보 풀 작성만 하고 실제 출원은 출원 잠금 해제 후로 연기 권장(현재 정책과 일치).

### 5.2 변리사 의뢰 단계 (Top 1~2 좁힌 후)

- **국내**: 한국 변리사에게 KIPRIS 정밀 검색(Class 9·42 + 유사군 코드) 의뢰. 1건당 보통 30~50만원, 1주.
- **미국**: 미국 변리사 또는 LegalZoom·Trademarkia 등 서비스를 통한 USPTO TESS 정밀 검색.
- **EU**: EUIPO eSearch (eSearch plus) 검색 — 한국 변리사가 협력 사무소 통해 의뢰 가능.
- **WIPO Madrid**: 추후 다국 출원 시 모니터링.

### 5.3 도메인 등록 (변리사 정밀 검색 통과 후, 출원 동시 진행)

- **Registrar 추천 (우선순위)**:
  1. **Cloudflare Registrar** — wholesale 가격(마진 0%), DNS·CDN 일체화, 보안 우수.
  2. **Namecheap** — 가격 합리적, 한국에서 결제 무난, WhoisGuard 무료.
  3. **Porkbun** — 일부 신규 TLD에서 가격 우위.
- **회피**: GoDaddy(가격↑·자동갱신 함정), 1domain·국내 영세 registrar(이전 어려움).
- **확보 권장 묶음** (Top 1 확정 시): `.com`(가능 시 고가 매입 검토) + `.io` + `.dev` + `.ai` + `.security` + KR `.kr`/`.co.kr`.

### 5.4 출원 시기

- D8-4(출원 잠금) 해제 = 변리사 정밀 검색 + 출원 동시 시작 시점.
- 출원 후 **6~8개월** 심사. 그 사이 GitHub public 전환 보류.
- 출원 완료 = 우선권 확보 → public 전환 가능 → README/모든 docs placeholder를 일괄 치환 (2026-05-18 Lodestar로 완료, Phase 7 R-BRAND Stage 1).

### 5.5 D1 확정 후 일괄 갱신 대상 파일 (참고)

- `README.md` (생성 예정)
- `docs/design/00-mission-and-positioning.md`
- `docs/design/README.md`
- `CLAUDE.md` (placeholder 표기 정리)
- `SESSION_HANDOFF.md` (D1 확정 일자·근거 결정 로그)
- 이 문서(`d1-brand-candidates.md`)에 "최종 결정: <Name>" append.

### 5.6 최종 결정 — Lodestar (2026-05-18, D-P7-1)

**확정 브랜드**: **Lodestar** (단독형)

**확정 일자**: 2026-05-18

**결정 식별자**: D-P7-1 (`docs/design/notes/phase7-public-transition-design.md` §3 R-BRAND + §11 D-P7-1)

**근거 요약** (design doc §3.2):
1. **등록 가능성 최우선** — Top 3 후보(Custos / Lodestar / Praxis) 중 보안·SW Class 9·42 dominant player 부재. LODESTAR Corp Class 42 = Dead/Cancelled(2010) 확인.
2. **메타포 가치** — "신뢰의 기준점(길잡이 별)" 메타포가 외부 감사인 검증 가치(§13.5 1순위 결합 청구항)와 직접 공명.
3. **한국어 표기 안정** — Custos("쿠스토스"/"커스토스")·Praxis("프락시스"/"프랙시스") 대비 "로드스타" 단일 표기.
4. **합성형 의존도 낮음** — 단독 단어로 등록 가능성 높아 brand 단순성 유지.

**트레이드오프 수용**:
- 차량 모델 연상(KG 쌍용 Lodestar) — Class 12 무관이라 출원 장애 X, 일반 사용자 첫 인상에서 차량 노이즈 minor.
- `.com`은 점유 — `.io` 또는 `.security` 또는 `.dev` 중 1개 확보 권장(D8 출원 완료 후).

**코드 네임스페이스 보존**: `rosshield` 유지. Go 모듈(`github.com/ssabro/rosshield`)·내부 패키지·CLI 명령(`rosshield`, `rosshield-server`, `rosshield-audit-verify`, `pack-tools`)·YAML apiVersion(`rosshield.io/v1`) 모두 변경 0. 사용자 대면 제품명 Lodestar / 코드네임 rosshield 양립.

**후속**: R-BRAND Stage 1 — 이전 commit `3e3d892`(사용자 대면 6 파일) + 본 commit(docs/design + docs/onboarding 잔여 + web i18n)으로 cover 완료.

---

## 부록 A: WebSearch 결과 신뢰도 (메모리 정책 준수 명시)

본 문서는 메모리 정책(`feedback_naming_verification.md`, "공개 식별자 확정 전 상표/제품 충돌을 WebSearch로 검증 필수")에 따라 12개 후보 전수 검증.

- **WebSearch 호출 횟수**: 11회 (12 후보 중 4개는 한 번에 묶어 검색).
- **점검 관할**: USPTO 1차(검색 엔진 인덱스 기반), KIPO·EUIPO·WIPO **미수행** — 모두 변리사 의뢰 항목으로 분류.
- **신뢰도**: WebSearch 결과는 **표면 신호**. 실제 등록 여부·심사 진행 상태·미등록 사용권(common law trademark)은 **변리사 정밀 검색에서만 확정**.
- **inconclusive 항목**: Custos/Vigil/Praxis/Lodestar의 KIPO 등록 여부 — 본 단계에서 결론 불가.

## 부록 B: 폐기 사례 학습 — "FleetGuard" 회고

- **충돌 1**: Cummins Filtration의 **FLEETGUARD®** (Class 7·12, 자동차·산업 필터) — 1948~ 사용, US·KR·EU·WIPO Madrid 다국 등록. Class 7·12라 우리 Class 9와 직접 충돌은 약하나, **저명상표 보호**로 광범위 권리 행사 가능성.
- **충돌 2**: Attestor.ai의 **FleetGuard** (Class 9·42, AI security) — 직접 경쟁 카테고리.
- **교훈 1**: **합성어는 두 단어 모두 검색**. "Fleet" + "Guard" 둘 다 흔한 보안·차량 어휘 → 충돌 다발 예측 가능했음.
- **교훈 2**: **공개 식별자(GitHub repo 이름·README) 확정 전 검증 필수**. fleetguard 디렉토리는 코드네임이라 유지하지만, 새 식별자 노출 전 동일 실수 반복 금지.
- **교훈 3**: 코드 네임스페이스(`rosshield`)와 사용자 대면 브랜드를 **분리 운영**한 결정은 옳았음. 향후 brand 변경이 있어도 코드는 영향 없음.
