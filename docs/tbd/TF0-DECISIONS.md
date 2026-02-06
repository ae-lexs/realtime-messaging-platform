# TF-0 TBD Decisions: Infrastructure Foundation

- **Status**: Approved
- **Date**: 2026-02-06
- **Related ADRs**: ADR-002 (Three-Plane Architecture), ADR-005 (WebSocket Protocol), ADR-014 (Technology Stack & Deployment)
- **Execution Plan Reference**: TF-0 Foundation

---

## Purpose

This document resolves the infrastructure decisions for TF-0, the Terraform foundation for the Realtime Messaging Platform. These decisions establish the networking, compute, and deployment baseline that all subsequent Terraform PRs (TF-1 through TF-3) build upon. Like PR0-DECISIONS.md, these are implementation specifications for patterns already mandated by the ADRs.

> **Normative Policy Layer**: This document defines **mandatory infrastructure conventions**. All Terraform code in this repository must conform to these specifications. Deviations require an ADR or explicit justification in the PR description with reviewer approval.

---

## TBD-TF0-1: Region & Availability Zones

### Problem Statement

ADR-014 §1 specifies single-region deployment but does not pin the region. The region choice affects latency to users, service availability history, and cost. AZ count determines fault tolerance granularity and NAT Gateway cost.

### Decision

- **Region**: `us-east-2` (Ohio)
- **AZ count**: 2
- **AZ selection**: Always via `data.aws_availability_zones` with `state = "available"` filter — never hardcoded AZ names

### Rationale

- **us-east-2** avoids the blast radius of `us-east-1` (historically the most incident-prone region) while remaining in the eastern US for low-latency access.
- **2 AZs** provides sufficient fault tolerance for the MVP while minimizing NAT Gateway cost (1–2 NAT GWs vs 3). Scaling to 3 AZs later requires only a variable change, not architectural rework.
- **Dynamic AZ lookup** prevents failures when AWS retires or adds AZs, and avoids hardcoded values per TERRAFORM.md invariants.

### Normative Rules

> **Rule: No Hardcoded AZ Names**
>
> All Terraform code must use `data.aws_availability_zones` to discover AZs. References like `us-east-2a` are prohibited in `.tf` files. This ensures portability and resilience to AZ changes.

---

## TBD-TF0-2: VPC & Network Topology

### Problem Statement

The platform requires public subnets (ALB, NAT Gateway) and private subnets (ECS tasks, VPC endpoints). The CIDR allocation must support future growth without re-IPing, and NAT Gateway configuration must balance cost (dev) against availability (prod).

### Decision

#### CIDR Allocation

| Component | CIDR | Usable IPs | Purpose |
|-----------|------|-----------|---------|
| VPC | `10.0.0.0/16` | 65,536 | Full address space |
| Public subnet AZ-a | `10.0.0.0/20` | 4,091 | ALB, NAT Gateway |
| Public subnet AZ-b | `10.0.16.0/20` | 4,091 | ALB, NAT Gateway |
| Private subnet AZ-a | `10.0.128.0/18` | 16,379 | ECS tasks, VPC endpoints |
| Private subnet AZ-b | `10.0.192.0/18` | 16,379 | ECS tasks, VPC endpoints |

#### NAT Gateway Strategy

- **Dev**: Single NAT Gateway (`single_nat_gateway = true`) — reduces cost from ~$96/mo to ~$48/mo
- **Prod**: Per-AZ NAT Gateway (`single_nat_gateway = false`) — eliminates cross-AZ single point of failure

#### VPC Gateway Endpoints

- **S3 Gateway Endpoint**: Free, reduces NAT charges for ECR image pulls and S3 access
- **DynamoDB Gateway Endpoint**: Free, reduces NAT charges for DynamoDB traffic (high-volume path)

### Rationale

- **/16 VPC** provides ample address space for future growth (additional subnets for databases, cache, etc.) without re-IPing.
- **Asymmetric subnet sizing** (/20 public vs /18 private) reflects that the vast majority of resources (ECS tasks) run in private subnets. Public subnets only host ALB and NAT GW.
- **Gateway endpoints** are free and eliminate NAT charges for the two highest-volume AWS services in this architecture (DynamoDB for persistence, S3 for ECR image layers).

### Normative Rules

> **Rule: Private Subnet for Compute**
>
> All ECS tasks must run in private subnets. No compute resource may be placed in a public subnet. Public subnets are exclusively for ALB and NAT Gateway. Private subnet route tables must never contain a route to an Internet Gateway — only routes to NAT Gateway and VPC endpoints. This makes the network boundary mechanically provable: a private subnet resource *cannot* receive inbound internet traffic regardless of security group misconfiguration.

> **Rule: Gateway Endpoints for Free Services**
>
> S3 and DynamoDB Gateway Endpoints must be created in every environment. These are free and reduce NAT Gateway data processing charges.

---

## TBD-TF0-3: Module Decomposition

### Problem Statement

TF-0 provisions ~50-60 resources. These must be organized into reusable modules that follow the project's Clean Architecture philosophy: clear boundaries, explicit interfaces, and no circular dependencies.

### Decision

| Module | Owns | Est. Resources | Dependencies |
|--------|------|---------------|--------------|
| `networking` | VPC, subnets, routes, NAT, IGW, endpoints, security groups | ~25 | None |
| `dns` | Route 53 zone, ACM cert + validation | ~5 | None |
| `ecr` | 4 ECR repos, lifecycle policies | ~8 | None |
| `ecs-cluster` | Cluster, capacity providers, Service Connect namespace | ~5 | None |
| `alb` | ALB, target groups, listeners, rules | ~15 | `networking` (VPC, subnets, SGs) |

### Rationale

- **5 modules** keeps complexity manageable while maintaining clear ownership boundaries.
- **`networking` includes security groups** because SGs are tightly coupled to the VPC and subnet topology — they reference each other and the VPC ID.
- **`alb` depends on `networking`** because the ALB needs the VPC ID, public subnet IDs, and security group IDs.
- **`dns`, `ecr`, `ecs-cluster` are independent** — they can be created in any order and have no cross-module dependencies.
- Total resources (~58) stay well under the 100-resource-per-state-file guideline from TERRAFORM.md.

### Normative Rules

> **Rule: Module Independence**
>
> Modules must communicate exclusively through variables and outputs. No module may use `terraform_remote_state` or hardcoded references to resources in another module. The root module is the sole composition point.

---

## TBD-TF0-4: Security Group Architecture

### Problem Statement

ADR-002 defines three planes (Connection, Durability, Fanout) with distinct communication patterns. Security groups must enforce these boundaries at the network level — a misconfigured SG that allows Fanout to write to DynamoDB directly (bypassing Ingest) would violate the architecture.

### Decision

6 security groups, one per logical component:

| Security Group | Inbound Rules | Outbound Rules |
|---------------|---------------|----------------|
| `sg-alb` | 443 from `0.0.0.0/0` (HTTPS) | 8080 → `sg-gateway`, 8083 → `sg-chatmgmt` |
| `sg-gateway` | 8080 from `sg-alb`, 8080 from `sg-fanout` | 9091 → `sg-ingest`, 6379 → `sg-redis` |
| `sg-ingest` | 9091 from `sg-gateway` | DynamoDB endpoint (prefix list), 9098 → `sg-msk` |
| `sg-fanout` | (none — pulls from Kafka, pushes to Redis) | 9098 → `sg-msk`, 6379 → `sg-redis`, 8080 → `sg-gateway` |
| `sg-chatmgmt` | 8083 from `sg-alb` | DynamoDB endpoint (prefix list), 9098 → `sg-msk`, 6379 → `sg-redis` |
| `sg-redis` | 6379 from `sg-gateway`, `sg-fanout`, `sg-chatmgmt` | Default egress |

**Note**: `sg-msk` is created as an empty shell in TF-0 — rules are populated in TF-2 when MSK is provisioned.

### Rationale

- **Per-component SGs** enforce ADR-002's plane boundaries at the network layer. The Fanout Worker cannot reach DynamoDB directly — it must go through Ingest or the DynamoDB VPC endpoint is only in SGs that need it.
- **sg-gateway allows inbound from sg-fanout** because Fanout delivers messages to Gateway via gRPC (ADR-002 §3).
- **sg-ingest only allows inbound from sg-gateway** because only Gateway calls the PersistMessage RPC (ADR-004).
- **sg-msk as empty shell** avoids forward-declaring MSK-specific rules before MSK exists. TF-2 populates it.
- **DynamoDB access via prefix list** rather than IP ranges, because VPC Gateway Endpoints use managed prefix lists that update automatically.

### Normative Rules

> **Rule: Security Groups Enforce Plane Boundaries**
>
> Security group rules must reflect the communication patterns in ADR-002. Adding a new inter-service communication path requires updating both the relevant ADR and the security group rules in the same PR.

> **Rule: No Broad Egress**
>
> No security group may have `0.0.0.0/0` egress on all ports. Egress rules must specify the destination SG or prefix list and the exact port.

> **Rule: Prefix-List Egress for AWS Services**
>
> All egress to AWS services (DynamoDB, S3) must use VPC Gateway Endpoint prefix lists — never IP ranges, CIDR blocks, or `0.0.0.0/0`. Prefix lists are the *only* permitted mechanism for AWS-service egress. This prevents future PRs from adding "temporary" broad egress rules that bypass endpoint routing. If a new AWS service requires egress (e.g., Secrets Manager in TF-1), it must go through a VPC Interface Endpoint with a corresponding security group, not a CIDR-based rule.

---

## TBD-TF0-5: ALB & DNS Configuration

### Problem Statement

The platform exposes WebSocket connections (Gateway) and REST APIs (Chat Mgmt) through a single ALB. ADR-005 requires long-lived WebSocket connections (heartbeat-based), ADR-014 §1.2 defines routing rules, and ADR-014 §1.2 prohibits sticky sessions.

### Decision

#### Domain & DNS

- **Domain**: Configurable via `domain_name` variable — no real domain purchased
- **Route 53 hosted zone**: Created regardless (required for ACM DNS validation)
- **ACM certificate**: Wildcard (`*.{domain}`) + apex (`{domain}`), DNS-validated within the same zone

#### ALB Configuration

| Setting | Value | Rationale |
|---------|-------|-----------|
| Idle timeout | 3600s | WebSocket connections per ADR-005; default 60s would kill idle WS connections |
| Sticky sessions | Disabled | Per ADR-014 §1.2 — WebSocket connections pin naturally after upgrade |
| Deregistration delay | 30s | Matches Gateway's graceful shutdown drain budget (3s delay + 20s HTTP + 5s OTEL = 28s) |
| HTTP → HTTPS | Redirect (301) | All traffic encrypted in transit per ADR-013 |

#### Listener Rules (Priority Order)

| Priority | Condition | Target | Notes |
|----------|-----------|--------|-------|
| 1 | Path `/ws` + WebSocket upgrade header | Gateway TG (8080) | WebSocket connections |
| 2 | Path `/api/v1/*` | Chat Mgmt TG (8083) | REST API via grpc-gateway |
| 3 | Path `/health` | Gateway TG (8080) | Public health check |
| Default | All other paths | Fixed 404 response | No catch-all backend |

#### Target Groups

| Target Group | Port | Protocol | Health Check | Deregistration Delay |
|-------------|------|----------|-------------|---------------------|
| Gateway | 8080 | HTTP | `GET /healthz` | 30s |
| Chat Mgmt | 8083 | HTTP | `GET /healthz` | 30s |

### Rationale

- **3600s idle timeout** prevents the ALB from killing WebSocket connections during quiet periods. Heartbeat at 30s interval (ADR-005 §6) keeps connections alive well within this window.
- **No sticky sessions** because WebSocket upgrade creates an implicit sticky connection — the TCP connection pins to the backend task. Cookie-based sticky sessions add complexity and interfere with rolling deployments.
- **30s deregistration delay** gives the Gateway time to send `connection_closing` frames and drain existing connections during deployments.
- **Default 404** prevents accidental exposure of internal services. Only explicitly routed paths are accessible.

### Normative Rules

> **Rule: ALB Idle Timeout ≥ 2 × Heartbeat Interval**
>
> The ALB idle timeout must always be at least twice the WebSocket heartbeat interval (currently 30s). If the heartbeat interval changes, the ALB timeout must be updated in the same PR.

> **Rule: No Sticky Sessions**
>
> Sticky sessions must never be enabled on the ALB. WebSocket connections pin naturally. Sticky sessions interfere with connection draining and rolling deployments.

---

## TBD-TF0-6: ECR Lifecycle & Image Strategy

### Problem Statement

Four services need container image repositories with lifecycle management to control storage costs and ensure image integrity.

### Decision

#### Repositories

4 ECR repos following the project naming convention:

| Repository | Service |
|-----------|---------|
| `messaging-platform-gateway` | Gateway Service |
| `messaging-platform-ingest` | Ingest Service |
| `messaging-platform-fanout` | Fanout Worker |
| `messaging-platform-chatmgmt` | Chat Mgmt Service |

#### Image Configuration

| Setting | Value | Rationale |
|---------|-------|-----------|
| Tag immutability | `IMMUTABLE` | ADR-014 §8.1 — prevents tag overwrite, ensures deploy reproducibility |
| Scan on push | Enabled | Security scanning for CVEs on every push |
| Encryption | AES-256 (default) | Sufficient for this use case; KMS adds cost without benefit |

#### Lifecycle Policy

- **Keep last 10 tagged images** — sufficient for rollback history
- **Expire untagged images after 7 days** — cleanup of intermediate/failed builds

### Rationale

- **IMMUTABLE tags** prevent the "latest tag was overwritten" class of deployment bugs. Every deployment references a unique, immutable tag (typically the git SHA).
- **10 tagged images** allows rolling back through ~10 deployments, which is ample for incident response.
- **7-day untagged retention** catches images that were pushed but never tagged (build failures, abandoned branches) while allowing time for debugging.

### Normative Rules

> **Rule: Immutable Tags**
>
> All ECR repositories must have tag immutability set to `IMMUTABLE`. No process may overwrite an existing image tag. CI/CD must tag images with the git SHA.

---

## TBD-TF0-7: ECS Cluster Configuration

### Problem Statement

The four services run on ECS Fargate (ADR-014 §1). The cluster configuration must support both cost-optimized dev and production-grade prod, with Service Connect for inter-service communication (ADR-014 §5.3).

### Decision

#### Cluster

- **Name**: `messaging-{environment}` (e.g., `messaging-dev`, `messaging-prod`)
- **Container Insights**: Enabled (CloudWatch Container Insights for resource utilization metrics)

#### Capacity Providers

| Provider | Dev | Prod | Rationale |
|----------|-----|------|-----------|
| `FARGATE` | Default (base: 0, weight: 1) | Default (base: 2, weight: 3) | On-demand for reliability |
| `FARGATE_SPOT` | Enabled (base: 0, weight: 3) | Disabled | 70% cost savings for dev; unacceptable interruption risk for prod |

#### Service Connect

- **Namespace**: `messaging.local` (per ADR-014 §5.3)
- **Service Connect**: Enabled at the cluster level
- Services register as:
  - `gateway.messaging.local:8080`
  - `ingest.messaging.local:9091`
  - `chatmgmt.messaging.local:8083`

#### Execute Command

- **Dev**: Enabled — allows `aws ecs execute-command` for debugging
- **Prod**: Disabled — no interactive shell access in production per security best practices

#### CloudWatch Log Group

- **Name**: `/ecs/messaging-{environment}`
- **Retention**: 30 days (dev), 90 days (prod)

### Rationale

- **Fargate Spot in dev only** provides significant cost savings (~70%) for development workloads where occasional task interruptions are acceptable. Production requires guaranteed capacity.
- **Container Insights** provides CPU/memory utilization metrics needed for capacity planning and auto-scaling decisions (referenced by TF-3).
- **Service Connect** provides client-side load balancing for gRPC connections, which is critical because gRPC multiplexes RPCs over a single HTTP/2 connection — server-side load balancing (ALB) would pin all RPCs to one backend.
- **Execute command in dev only** follows the principle of least privilege. Production debugging uses structured logs and traces (ADR-012), not interactive shells.

### Normative Rules

> **Rule: No Fargate Spot in Production**
>
> Production capacity provider strategy must use `FARGATE` only. Spot interruptions during message processing could cause data loss between DynamoDB write and Kafka publish (ADR-004 Step 4→5 gap).

> **Rule: Service Connect Namespace**
>
> All services must register in the `messaging.local` namespace. Service discovery names follow the pattern `{service}.messaging.local`.

---

## TBD-TF0-8: State Backend & Versioning

### Problem Statement

Terraform state must be stored remotely with locking to prevent concurrent modifications. The state backend itself cannot be managed by Terraform (chicken-and-egg problem) and requires a bootstrap process.

### Decision

#### State Backend

| Setting | Value |
|---------|-------|
| S3 bucket | `messaging-platform-terraform-state` |
| Region | `us-east-2` |
| Lock table | `terraform-locks` (DynamoDB) |
| Encryption | SSE-S3 (`encrypt = true`) |
| Versioning | Enabled (state recovery) |
| Public access | Blocked (all four block settings) |
| State key pattern | `{environment}/terraform.tfstate` |

#### Bootstrap

The state backend is provisioned via `scripts/bootstrap-terraform-state.sh` — an idempotent shell script that:

1. Creates the S3 bucket (if not exists) with versioning, encryption, and public access block
2. Creates the DynamoDB table (if not exists) with PAY_PER_REQUEST billing
3. Is safe to re-run (all operations are conditional)

This solves the chicken-and-egg problem: you can't use Terraform to manage the bucket that stores Terraform's state.

#### Version Constraints

| Component | Constraint | Location |
|-----------|-----------|----------|
| Terraform CLI | `>= 1.9.0, < 2.0.0` | Root modules `versions.tf` |
| AWS Provider (root) | `~> 6.31.0` | Root modules `versions.tf` |
| AWS Provider (child) | `>= 6.0` | Child modules `versions.tf` |

### Rationale

- **S3 + DynamoDB** is the standard Terraform remote backend for AWS. S3 provides durable state storage; DynamoDB provides consistent locking to prevent concurrent `terraform apply`.
- **Versioning** enables state recovery if a `terraform apply` corrupts state.
- **Separate state per environment** prevents a dev `terraform destroy` from affecting prod state.
- **`>= 1.9.0, < 2.0.0`** allows any Terraform 1.x from 1.9 onward. The 1.9 floor ensures features like `import` blocks and `removed` blocks are available. The 2.0 ceiling prevents automatic upgrades to a major version that may have breaking changes.
- **`~> 6.31.0`** in root modules pins to the 6.31.x patch line — allows bug fixes but prevents minor version upgrades that might introduce new resource behaviors. The 6.x line is a greenfield choice (no migration cost from 5.x).
- **`>= 6.0`** in child modules provides broad compatibility so root modules can upgrade the provider independently.
- **Bootstrap script** is the standard pattern for Terraform state backends. It's intentionally simple (AWS CLI only) and idempotent.

### Normative Rules

> **Rule: State Backend Region**
>
> The Terraform state backend must be in `us-east-2`, co-located with the infrastructure it manages. This minimizes latency during `terraform plan/apply` operations.

> **Rule: Bootstrap Before Init**
>
> `scripts/bootstrap-terraform-state.sh` must be run once per AWS account before `terraform init`. CI/CD pipelines must ensure the bootstrap has been executed.

> **Rule: Lock File Committed**
>
> `.terraform.lock.hcl` must be committed to version control per TERRAFORM.md invariants. The `.gitignore` must not exclude it.

> **Rule: Backend Resources Are Outside Terraform**
>
> No Terraform code may reference, import, or manage the state backend resources (the `messaging-platform-terraform-state` S3 bucket and `terraform-locks` DynamoDB table). These resources exist outside the Terraform dependency graph by design. Importing them creates a circular dependency where destroying state would destroy the state backend. The bootstrap script is the sole management interface for these resources.

---

## Summary of Decisions

| TBD | Decision | Key Points |
|-----|----------|------------|
| **TBD-TF0-1** | Region us-east-2, 2 AZs | Avoids us-east-1 blast radius; dynamic AZ lookup |
| **TBD-TF0-2** | VPC 10.0.0.0/16, asymmetric subnets | /20 public, /18 private; single vs per-AZ NAT |
| **TBD-TF0-3** | 5 modules: networking, dns, ecr, ecs-cluster, alb | Clear boundaries, ~58 total resources |
| **TBD-TF0-4** | 6 security groups per ADR-002 planes | Network-layer enforcement of architecture boundaries |
| **TBD-TF0-5** | ALB with 3600s idle timeout, no sticky sessions | WebSocket-compatible; 4 routing rules |
| **TBD-TF0-6** | 4 ECR repos with immutable tags | 10 tagged retention, 7-day untagged expiry |
| **TBD-TF0-7** | ECS Fargate + Spot (dev only), Service Connect | messaging.local namespace; Container Insights |
| **TBD-TF0-8** | S3 + DynamoDB state backend, bootstrap script | us-east-2; provider 6.31.0; TF >= 1.9.0 |

---

## Validation Checklist

### Networking (TBD-TF0-1, TBD-TF0-2)
- [ ] VPC created with `10.0.0.0/16` CIDR
- [ ] 2 public + 2 private subnets across 2 AZs
- [ ] NAT Gateway count matches environment (1 for dev, 2 for prod)
- [ ] S3 and DynamoDB Gateway Endpoints created
- [ ] No hardcoded AZ names in any `.tf` file

### Security Groups (TBD-TF0-4)
- [ ] 6 security groups created with correct inbound/outbound rules
- [ ] No security group has `0.0.0.0/0` egress on all ports
- [ ] sg-msk exists as empty shell

### ALB & DNS (TBD-TF0-5)
- [ ] ALB idle timeout = 3600s
- [ ] Sticky sessions disabled
- [ ] HTTP → HTTPS redirect configured
- [ ] 4 listener rules in correct priority order
- [ ] ACM certificate with wildcard + apex SANs

### ECR (TBD-TF0-6)
- [ ] 4 repositories with IMMUTABLE tag immutability
- [ ] Lifecycle policies: 10 tagged, 7-day untagged
- [ ] Scan on push enabled

### ECS Cluster (TBD-TF0-7)
- [ ] Cluster with Container Insights enabled
- [ ] Fargate + Fargate Spot capacity providers (dev), Fargate only (prod)
- [ ] Service Connect namespace `messaging.local` created
- [ ] CloudWatch log group created with correct retention

### State Backend (TBD-TF0-8)
- [ ] Bootstrap script creates S3 bucket + DynamoDB table
- [ ] Bootstrap script is idempotent (safe to re-run)
- [ ] `.terraform.lock.hcl` not in `.gitignore`
- [ ] Provider version `~> 6.31.0` in root modules, `>= 6.0` in child modules

---

## References

### AWS Documentation
- [VPC Gateway Endpoints](https://docs.aws.amazon.com/vpc/latest/privatelink/gateway-endpoints.html)
- [ALB Idle Timeout](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/application-load-balancers.html#connection-idle-timeout)
- [ECS Service Connect](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/service-connect.html)
- [ECR Lifecycle Policies](https://docs.aws.amazon.com/AmazonECR/latest/userguide/LifecyclePolicies.html)
- [S3 Backend for Terraform](https://developer.hashicorp.com/terraform/language/settings/backends/s3)

### Project References
- [ADR-002: Three-Plane Architecture](../adr/ADR-002.md)
- [ADR-005: WebSocket Protocol](../adr/ADR-005.md)
- [ADR-014: Technology Stack & Deployment](../adr/ADR-014.md)
- [TERRAFORM.md: Terraform Standards](../standards/TERRAFORM.md)
- [PR0-DECISIONS.md: Configuration, Error Taxonomy & Clock](PR0-DECISIONS.md)
