#!/usr/bin/env bash
# Substrate gate: no AWS in anything executable or authoritative (ADR-021).
#
# M0.1's version grepped `aws-sdk-go` in *.go and nothing else. That is how a
# 200-line `aws secretsmanager` / `aws ssm` CLI script survived the migration
# untouched until M1.2, and how comments went on describing GCP code in terms of
# AWS services long enough to make internal/auth/remote_keys.go read as an AWS
# implementation to someone reviewing it.
#
# So the gate covers imports, invocations, resource types AND prose, across
# every file that tells someone how the system works today:
#
#   Go (excluding generated gen/), Terraform, shell scripts, Kubernetes
#   manifests, protobuf, and the root config files.
#
# docs/ is deliberately exempt. The ADRs *are* the migration's historical
# record — ADR-007's superseded DynamoDB model, ADR-015's Appendix F mapping,
# the retired TF-0/TF-1 decisions — and scrubbing AWS from them would destroy
# the reasoning this project exists to keep. The rule is therefore: **explain a
# GCP construct by what it is, and cite the ADR for what it used to be.**
#
# Usage: scripts/check-no-aws.sh
set -uo pipefail

# Executable AWS: imports, CLI calls, providers, resource ARNs.
readonly EXECUTABLE='aws-sdk-go|hashicorp/aws|arn:aws:|amazonaws\.com|(^|[^a-zA-Z_-])aws (configure|s3|ssm|kms|ecs|iam|sns|sqs|dynamodb|secretsmanager|sts|logs) '

# AWS service vocabulary in prose. Word-bounded so "flaws", "laws" and the
# Spanish "ecs-" prefixes do not trip it.
readonly VOCABULARY='\bAWS\b|\bSSM\b|Secrets Manager|Parameter Store|\bDynamoDB\b|\bLocalStack\b|\bCloudWatch\b|Amazon [A-Z]'

# Ambiguous on their own — flagged only outside docs, where they almost always
# mean the AWS product rather than the generic term.
readonly AMBIGUOUS='\bSNS\b|\bECS\b|\bALB\b|\bMSK\b|\bEKS\b|\bEC2\b'

# Files a developer reads to work out how the code behaves: Go, Terraform,
# shell, protobuf, manifests, compose. Generated code is excluded because it
# mirrors proto, which is checked. This script is excluded because it has to
# name what it forbids.
#
# Markdown is excluded on purpose, and not as a loophole. README and
# CONTRIBUTING carry the project's narrative — "the platform was migrated from
# AWS to GCP" is the headline, not a leak — and docs/ is the historical record
# itself. Neither can mislead someone about what the code in front of them
# talks to, which is the failure this gate exists to prevent.
files() {
  git ls-files \
    '*.go' '*.tf' '*.sh' '*.proto' '*.yaml' '*.yml' \
    | grep -v '^gen/' \
    | grep -v '^docs/' \
    | grep -v '^scripts/check-no-aws.sh$'
}

violations=0
report() {
  local label="$1" pattern="$2"
  local hits
  hits="$(files | xargs grep -nEI "${pattern}" 2>/dev/null || true)"
  if [[ -n "${hits}" ]]; then
    echo "❌ ${label}:"
    echo "${hits}" | sed 's/^/   /'
    violations=1
  fi
}

report "AWS SDK imports, CLI invocations or resource ARNs" "${EXECUTABLE}"
report "AWS service names in prose — describe the GCP construct, cite the ADR for history" "${VOCABULARY}"
report "AWS service acronyms" "${AMBIGUOUS}"

if [[ "${violations}" -ne 0 ]]; then
  echo
  echo "The substrate is GCP (ADR-021). Explain what a thing IS, and cite the ADR"
  echo "for what it replaced — docs/ holds the history, code does not."
  exit 1
fi

echo "✅ no AWS references outside docs/"
