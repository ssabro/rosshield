# Patroni Auto-Failover 운영 가이드 (Phase 9 Stage 9.2)

> **대상**: Kubernetes 운영자 — Lodestar enterprise customer.
> **선행**: [`multi-region-ha-design.md`](../design/notes/multi-region-ha-design.md) §5 Stage 6 + [`auto-failover-research.md`](../design/notes/auto-failover-research.md) D-AF-1=A 수용.
> **목표**: PG primary 장애 시 자동 promote(RTO ≤ 60초) + Lodestar E25 RoleProvider를 Patroni REST로 swap하여 단일 source of truth 유지.
> **결정**: D-AF-1=A Patroni / D-AF-2=A RoleProvider swap / D-AF-3=A timeline / D-AF-4=A `--ha-rp=e25` fallback.

---

## 1. 사전 준비

### 1.1 인프라 요구

- Kubernetes cluster (v1.28+) — region 1개 또는 multi-region
- etcd cluster:
  - **권장 A**: Kubernetes 자체 etcd 재사용 (kube-apiserver 의존, 별 운영 부담 0)
  - **권장 B**: 별 etcd cluster (3 노드, region별)
- StorageClass — PG PVC용 (region-local SSD 권장)
- LoadBalancer — Patroni REST + PG 5432 expose

### 1.2 권장 Helm chart

```bash
# Option 1: Bitnami Patroni (안정성 + Kubernetes-native)
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update

# Option 2: Cybertec Postgres Operator (advanced features)
helm repo add postgres-operator-charts https://opensource.cybertec.at/charts
```

본 가이드는 **Bitnami Patroni**를 기준으로 합니다 (단순 + 정착도).

---

## 2. Patroni 배포

### 2.1 namespace 준비

```bash
kubectl create namespace rosshield-ha
kubectl label namespace rosshield-ha env=prod
```

### 2.2 values 작성

`deploy/k8s/patroni/values-example.yaml`을 복사 후 customer 환경에 맞춰 편집:

```bash
cd deploy/k8s/patroni
cp values-example.yaml values-prod.yaml
# values-prod.yaml 편집 (replicaCount · storageClass · resources · etcd 설정)
```

### 2.3 install

```bash
helm install rosshield-patroni bitnami/postgresql-ha \
  --namespace rosshield-ha \
  --values deploy/k8s/patroni/values-prod.yaml \
  --version 13.0.0  # major 고정 권장
```

### 2.4 검증

```bash
# 모든 Patroni pod ready
kubectl get pods -n rosshield-ha
# Expected:
# rosshield-patroni-postgresql-ha-pgpool-...    Running
# rosshield-patroni-postgresql-ha-postgresql-0  Running   # primary
# rosshield-patroni-postgresql-ha-postgresql-1  Running   # standby
# rosshield-patroni-postgresql-ha-postgresql-2  Running   # standby

# Patroni REST endpoint
kubectl port-forward -n rosshield-ha svc/rosshield-patroni-postgresql-ha-postgresql 8008:8008 &
curl http://localhost:8008/master
# Expected: 200 OK, body에 leader 정보

curl http://localhost:8008/cluster | jq
# Expected: members 배열 + leader + timeline
```

---

## 3. Lodestar rosshield-server 통합

### 3.1 RoleProvider swap

`--ha-rp=patroni` flag로 E25 PG advisory lock 대신 Patroni REST polling 사용:

```bash
./rosshield-server \
  --ha-enabled \
  --ha-rp=patroni \
  --ha-patroni-url=http://rosshield-patroni-postgresql-ha-postgresql.rosshield-ha.svc.cluster.local:8008 \
  --ha-patroni-poll-interval=1s \
  --replication-enabled \
  --replication-role=primary  # Patroni가 실 leader 식별, role flag는 hint
```

env:
```bash
ROSSHIELD_HA_RP=patroni
ROSSHIELD_HA_PATRONI_URL=http://patroni:8008
ROSSHIELD_HA_PATRONI_POLL_INTERVAL=1s
```

### 3.2 rosshield-server Kubernetes Deployment

`deploy/k8s/patroni/rosshield-deployment.yaml` 예시:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rosshield-server
  namespace: rosshield-ha
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: rosshield-server
        image: ssabro/rosshield-server:v0.7.9
        env:
        - name: ROSSHIELD_HA_ENABLED
          value: "1"
        - name: ROSSHIELD_HA_RP
          value: "patroni"
        - name: ROSSHIELD_HA_PATRONI_URL
          value: "http://rosshield-patroni-postgresql-ha-postgresql:8008"
        - name: ROSSHIELD_REPLICATION_ENABLED
          value: "1"
        - name: ROSSHIELD_DB_DSN
          value: "postgres://rosshield@rosshield-patroni-postgresql-ha-postgresql:5432/rosshield?sslmode=require"
        ports:
        - containerPort: 8080
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
        readinessProbe:
          httpGet:
            path: /healthz
            port: 8080
```

### 3.3 모든 pod의 RoleProvider 동일 source

3 replica pod 모두 Patroni REST poll → 1 pod만 leader (Patroni가 식별한 PG primary 연결) → 나머지 follower. application-level write는 `RequireLeaderForWrites` middleware가 follower에서 503 반환.

---

## 4. Failover 시나리오

### 4.1 자동 promote (Patroni 표준)

```
T+0:00  PG primary pod 죽음 (kubelet 또는 PG crash)
T+0:05  Patroni leader lease 만료 감지 (5초 TTL)
T+0:10  Patroni standby × 2가 promote 시도 → etcd quorum 확인 → 1개 promote
T+0:15  새 PG primary가 wal stream 수용 시작
T+0:20  rosshield-server pod 3개가 Patroni REST polling → 새 leader 식별
T+0:21  application-level RoleProvider epoch 증가 → write API 수용 시작
T+0:21  ALB target group이 새 leader pod로 routing (readiness probe 200)
```

**RTO: ~30초** (etcd quorum + Patroni lease + rosshield poll 합).

### 4.2 manual override

운영자가 Patroni 자동 cutover를 일시 정지하고 수동 promote:

```bash
# 자동 failover 정지
kubectl exec -n rosshield-ha rosshield-patroni-postgresql-ha-postgresql-0 -- \
  patronictl pause

# 수동 promote (특정 노드를 leader로 강제)
kubectl exec -n rosshield-ha rosshield-patroni-postgresql-ha-postgresql-1 -- \
  patronictl failover --candidate rosshield-patroni-postgresql-ha-postgresql-1

# 자동 failover 재활성
kubectl exec -n rosshield-ha rosshield-patroni-postgresql-ha-postgresql-0 -- \
  patronictl resume
```

### 4.3 air-gap customer fallback (D-AF-4)

Patroni 거부 환경 (air-gap · single PG · dep 추가 거부 customer):

```bash
./rosshield-server \
  --ha-enabled \
  --ha-rp=e25  # 기존 PG advisory lock 사용 (Patroni 의존 0)
```

기존 E25 동작 그대로 유지. customer drill에서는 rosshield-server restart 후 leader-election 시간 ~10-15초.

---

## 5. 모니터링

### 5.1 Patroni 메트릭 (Prometheus exporter)

Bitnami Patroni Helm chart는 prometheus-postgres-exporter sidecar 포함. 메트릭 예시:

- `pg_replication_lag` — replication lag (초)
- `pg_up` — PG 인스턴스 ready
- `patroni_master` — 본 노드가 leader인지

### 5.2 Lodestar 메트릭 (기존)

- `rosshield_ha_role` — Lodestar의 leader/follower 상태 (Patroni 결정 반영)
- `rosshield_ha_leader_epoch` — Patroni timeline 자동 반영 (Phase 9.3에서 결선)
- `rosshield_ha_failover_total` — 누적 failover 횟수
- `rosshield_replication_lag_seconds` — primary 측 PG lag (HA leader-only gate 적용)

### 5.3 Alertmanager rule

```yaml
groups:
- name: rosshield-patroni
  rules:
  - alert: PatroniLeaderMissing
    expr: count(patroni_master == 1) != 1
    for: 30s
    annotations:
      summary: "Patroni cluster has no leader or multiple leaders — split-brain risk"

  - alert: PatroniFailoverFrequent
    expr: rate(rosshield_ha_failover_total[1h]) > 3
    annotations:
      summary: "Patroni failover 1시간에 3회 초과 — flapping 의심"
```

---

## 6. 한계 / open issues

### 6.1 cross-region etcd latency

3 region 분산 etcd quorum write는 majority RTT 가산. 권장: **region-local etcd cluster** 운영, cross-region은 별 layer(DNS routing — Stage 4)가 처리.

### 6.2 split-brain edge case

network partition 시 minority quorum이 false promote 시도. Patroni의 watchdog STONITH(Linux kernel softdog) 활성화로 자체 PG process kill — 추가 카운트.

`values.yaml`에:
```yaml
postgresql:
  watchdog:
    enabled: true
    mode: required  # softdog 없으면 promote 거부
```

### 6.3 Lodestar 통합 carryover

본 가이드 가정한 Lodestar 측 코드 변경은 Stage 9.3~9.4 carryover:
- `internal/platform/ha/patroni/` RoleProvider 구현
- bootstrap `--ha-rp` flag + URL/interval config
- `ROSSHIELD_HA_RP=patroni` env

v0.7.9 이전 binary는 `--ha-rp=patroni` flag 미지원 — Phase 9.3 release 진입 후 customer 적용 가능.

### 6.4 Patroni Python dep

binary 1개 추가 (Patroni Python virtualenv 또는 Docker image). customer가 Python 거부하면 `--ha-rp=e25` fallback.

---

## 7. 참조

- design [`auto-failover-research.md`](../design/notes/auto-failover-research.md) — 옵션 비교 + 결정 근거
- design [`multi-region-ha-design.md`](../design/notes/multi-region-ha-design.md) — Phase 8 epic
- Patroni docs: https://patroni.readthedocs.io
- Bitnami PostgreSQL HA Helm chart: https://github.com/bitnami/charts/tree/main/bitnami/postgresql-ha
- values sample: [`deploy/k8s/patroni/values-example.yaml`](../../deploy/k8s/patroni/values-example.yaml)
- rosshield deployment sample: [`deploy/k8s/patroni/rosshield-deployment.yaml`](../../deploy/k8s/patroni/rosshield-deployment.yaml)
