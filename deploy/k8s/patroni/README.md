# Patroni Auto-Failover Kubernetes Deployment

> **Phase 9 Stage 9.2 산출** — Patroni Helm chart values + rosshield-server Deployment 통합 예시.
> **선행**: [`docs/operations/patroni-deployment.md`](../../../docs/operations/patroni-deployment.md) 정독.
> **결정**: D-AF-1=A Patroni / D-AF-2=A RoleProvider swap / D-AF-3=A timeline / D-AF-4=A `--ha-rp=e25` fallback.

---

## 구조

```
deploy/k8s/patroni/
├── README.md                       # 본 문서
├── values-example.yaml             # Bitnami postgresql-ha Helm chart values
└── rosshield-deployment.yaml       # rosshield-server Deployment + Service + Ingress
```

## 사용법

### 1. namespace + secret

```bash
kubectl create namespace rosshield-ha
kubectl label namespace rosshield-ha env=prod
```

### 2. Patroni Helm chart 설치

```bash
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update

cp values-example.yaml values-prod.yaml
# values-prod.yaml 편집 (storageClass · resources · region 등)

helm install rosshield-patroni bitnami/postgresql-ha \
  --namespace rosshield-ha \
  --values values-prod.yaml \
  --version 13.0.0
```

### 3. rosshield-server 배포

```bash
# rosshield-deployment.yaml 편집:
# - rosshield-pg-secret.password = 강한 비밀번호
# - Ingress host = customer 도메인
# - image tag = customer가 빌드한 image

kubectl apply -f rosshield-deployment.yaml
```

### 4. 검증

```bash
# Patroni cluster ready
kubectl get pods -n rosshield-ha
# Expected: 3 patroni + 3 rosshield-server pods Running

# Patroni leader 식별
kubectl exec -n rosshield-ha rosshield-patroni-postgresql-ha-postgresql-0 -- patronictl list
# Expected: 1 Leader + 2 Replica

# rosshield-server healthz
kubectl port-forward -n rosshield-ha svc/rosshield-server 8080:80 &
curl http://localhost:8080/healthz | jq '.ha'
# Expected: { "enabled": true, "role": "leader|follower", "epoch": ... }
```

## Failover 시나리오

### 자동 promote (Patroni 표준)

Patroni가 PG primary down 감지 시 자동 promote — RTO ~30초. rosshield-server는 Patroni REST polling으로 새 leader 자동 추종.

### Manual override

```bash
# 자동 failover 일시 정지
kubectl exec -n rosshield-ha rosshield-patroni-postgresql-ha-postgresql-0 -- patronictl pause

# 수동 promote
kubectl exec -n rosshield-ha rosshield-patroni-postgresql-ha-postgresql-1 -- \
  patronictl failover --candidate rosshield-patroni-postgresql-ha-postgresql-1

# 재활성
kubectl exec -n rosshield-ha rosshield-patroni-postgresql-ha-postgresql-0 -- patronictl resume
```

### air-gap fallback (D-AF-4)

Patroni 거부 환경에서는 `ROSSHIELD_HA_RP=e25`로 변경 후 redeploy. rosshield-server가 기존 E25 PG advisory lock 사용 (Patroni dep 0).

---

## 한계

- 본 manifest는 reference — customer는 자체 cluster 환경에 맞춰 image tag · resources · network policy 등 조정 필요.
- Patroni Python dep — air-gap 거부 customer는 D-AF-4 fallback.
- Lodestar Phase 9.3~9.4 코드 변경(`--ha-rp=patroni` flag + Patroni RoleProvider) 후 본 manifest의 `ROSSHIELD_HA_RP=patroni` 실 적용 가능. v0.7.9 이전 binary는 본 env 무시.

---

## 참조

- ops [`patroni-deployment.md`](../../../docs/operations/patroni-deployment.md)
- design [`auto-failover-research.md`](../../../docs/design/notes/auto-failover-research.md)
- Bitnami chart: https://github.com/bitnami/charts/tree/main/bitnami/postgresql-ha
- Patroni docs: https://patroni.readthedocs.io
