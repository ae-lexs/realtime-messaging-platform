# Networking module — VPC, subnets, NAT, IGW, VPC endpoints
#
# Implements TBD-TF0-1 (Region & AZs) and TBD-TF0-2 (VPC & Network Topology).
# Private subnets have no IGW routes — only NAT and VPC endpoint routes.

# -----------------------------------------------------------------------------
# Data sources
# -----------------------------------------------------------------------------

data "aws_availability_zones" "available" {
  state = "available"
}

data "aws_region" "current" {}

locals {
  azs       = slice(data.aws_availability_zones.available.names, 0, var.az_count)
  name      = "${var.project_name}-${var.environment}"
  nat_count = var.single_nat_gateway ? 1 : var.az_count
}

# -----------------------------------------------------------------------------
# VPC
# -----------------------------------------------------------------------------

resource "aws_vpc" "main" {
  cidr_block           = var.vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = {
    Name = "${local.name}-vpc"
  }
}

# -----------------------------------------------------------------------------
# Internet Gateway
# -----------------------------------------------------------------------------

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id

  tags = {
    Name = "${local.name}-igw"
  }
}

# -----------------------------------------------------------------------------
# Public Subnets
# -----------------------------------------------------------------------------

resource "aws_subnet" "public" {
  for_each = { for i, az in local.azs : az => {
    cidr = var.public_subnet_cidrs[i]
    az   = az
  } }

  vpc_id                  = aws_vpc.main.id
  cidr_block              = each.value.cidr
  availability_zone       = each.value.az
  map_public_ip_on_launch = false

  tags = {
    Name = "${local.name}-public-${each.key}"
    Tier = "public"
  }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  tags = {
    Name = "${local.name}-public-rt"
  }
}

resource "aws_route" "public_internet" {
  route_table_id         = aws_route_table.public.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.main.id
}

resource "aws_route_table_association" "public" {
  for_each = aws_subnet.public

  subnet_id      = each.value.id
  route_table_id = aws_route_table.public.id
}

# -----------------------------------------------------------------------------
# Private Subnets
# -----------------------------------------------------------------------------

resource "aws_subnet" "private" {
  for_each = { for i, az in local.azs : az => {
    cidr = var.private_subnet_cidrs[i]
    az   = az
  } }

  vpc_id            = aws_vpc.main.id
  cidr_block        = each.value.cidr
  availability_zone = each.value.az

  tags = {
    Name = "${local.name}-private-${each.key}"
    Tier = "private"
  }
}

# One route table per AZ for private subnets (each points to its own NAT GW,
# or all share a single NAT GW when single_nat_gateway = true).
resource "aws_route_table" "private" {
  for_each = { for i, az in local.azs : az => az }

  vpc_id = aws_vpc.main.id

  tags = {
    Name = "${local.name}-private-rt-${each.key}"
  }
}

resource "aws_route" "private_nat" {
  for_each = { for i, az in local.azs : az => i }

  route_table_id         = aws_route_table.private[each.key].id
  destination_cidr_block = "0.0.0.0/0"
  nat_gateway_id         = aws_nat_gateway.main[local.azs[var.single_nat_gateway ? 0 : each.value]].id
}

resource "aws_route_table_association" "private" {
  for_each = aws_subnet.private

  subnet_id      = each.value.id
  route_table_id = aws_route_table.private[each.key].id
}

# -----------------------------------------------------------------------------
# NAT Gateway(s) + EIPs
# -----------------------------------------------------------------------------

resource "aws_eip" "nat" {
  for_each = { for i, az in local.azs : az => az if i < local.nat_count }

  domain = "vpc"

  tags = {
    Name = "${local.name}-nat-eip-${each.key}"
  }
}

resource "aws_nat_gateway" "main" {
  for_each = aws_eip.nat

  allocation_id = each.value.id
  subnet_id     = aws_subnet.public[each.key].id

  tags = {
    Name = "${local.name}-nat-${each.key}"
  }

  depends_on = [aws_internet_gateway.main]
}

# -----------------------------------------------------------------------------
# VPC Gateway Endpoints (free — reduces NAT charges)
# TBD-TF0-2: S3 and DynamoDB Gateway Endpoints are mandatory.
# -----------------------------------------------------------------------------

resource "aws_vpc_endpoint" "s3" {
  vpc_id       = aws_vpc.main.id
  service_name = "com.amazonaws.${data.aws_region.current.region}.s3"

  vpc_endpoint_type = "Gateway"
  route_table_ids   = [for rt in aws_route_table.private : rt.id]

  tags = {
    Name = "${local.name}-s3-endpoint"
  }
}

resource "aws_vpc_endpoint" "dynamodb" {
  vpc_id       = aws_vpc.main.id
  service_name = "com.amazonaws.${data.aws_region.current.region}.dynamodb"

  vpc_endpoint_type = "Gateway"
  route_table_ids   = [for rt in aws_route_table.private : rt.id]

  tags = {
    Name = "${local.name}-dynamodb-endpoint"
  }
}

# -----------------------------------------------------------------------------
# VPC Interface Endpoints (TBD-TF1-6: Auth services)
# Secrets Manager, SSM, and KMS — required for ECS tasks in private subnets.
# Controlled by enable_vpc_interface_endpoints toggle.
# -----------------------------------------------------------------------------

locals {
  interface_endpoint_services = var.enable_vpc_interface_endpoints ? {
    secretsmanager = "com.amazonaws.${data.aws_region.current.region}.secretsmanager"
    ssm            = "com.amazonaws.${data.aws_region.current.region}.ssm"
    kms            = "com.amazonaws.${data.aws_region.current.region}.kms"
  } : {}
}

resource "aws_vpc_endpoint" "interface" {
  for_each = local.interface_endpoint_services

  vpc_id            = aws_vpc.main.id
  service_name      = each.value
  vpc_endpoint_type = "Interface"

  subnet_ids         = [for s in aws_subnet.private : s.id]
  security_group_ids = [aws_security_group.vpc_endpoints[0].id]

  private_dns_enabled = true

  tags = {
    Name = "${local.name}-${each.key}-endpoint"
  }
}
