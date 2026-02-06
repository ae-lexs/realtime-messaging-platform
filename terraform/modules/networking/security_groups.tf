# Security groups — one per logical component per TBD-TF0-4.
#
# These enforce ADR-002 plane boundaries at the network layer:
# - Connection Plane: sg_alb, sg_gateway
# - Durability Plane: sg_ingest
# - Fanout Plane: sg_fanout
# - Data stores: sg_redis, sg_msk (empty shell — populated in TF-2)
#
# No security group has 0.0.0.0/0 egress on all ports.
# AWS-service egress uses VPC Gateway Endpoint prefix lists only.

# -----------------------------------------------------------------------------
# Prefix lists for VPC Gateway Endpoints
# -----------------------------------------------------------------------------

data "aws_prefix_list" "s3" {
  name = "com.amazonaws.${data.aws_region.current.name}.s3"
}

data "aws_prefix_list" "dynamodb" {
  name = "com.amazonaws.${data.aws_region.current.name}.dynamodb"
}

# -----------------------------------------------------------------------------
# sg-alb — Application Load Balancer
# -----------------------------------------------------------------------------

resource "aws_security_group" "alb" {
  name_prefix = "${local.name}-alb-"
  description = "ALB: HTTPS inbound from internet, outbound to Gateway and ChatMgmt"
  vpc_id      = aws_vpc.main.id

  lifecycle {
    create_before_destroy = true
  }

  tags = {
    Name = "${local.name}-alb"
  }
}

resource "aws_vpc_security_group_ingress_rule" "alb_https" {
  security_group_id = aws_security_group.alb.id
  description       = "HTTPS from internet"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_vpc_security_group_egress_rule" "alb_to_gateway" {
  security_group_id            = aws_security_group.alb.id
  description                  = "Forward to Gateway (HTTP/WS)"
  ip_protocol                  = "tcp"
  from_port                    = 8080
  to_port                      = 8080
  referenced_security_group_id = aws_security_group.gateway.id
}

resource "aws_vpc_security_group_egress_rule" "alb_to_chatmgmt" {
  security_group_id            = aws_security_group.alb.id
  description                  = "Forward to Chat Mgmt (REST)"
  ip_protocol                  = "tcp"
  from_port                    = 8083
  to_port                      = 8083
  referenced_security_group_id = aws_security_group.chatmgmt.id
}

# -----------------------------------------------------------------------------
# sg-gateway — Gateway Service (Connection Plane)
# -----------------------------------------------------------------------------

resource "aws_security_group" "gateway" {
  name_prefix = "${local.name}-gateway-"
  description = "Gateway: inbound from ALB and Fanout, outbound to Ingest and Redis"
  vpc_id      = aws_vpc.main.id

  lifecycle {
    create_before_destroy = true
  }

  tags = {
    Name = "${local.name}-gateway"
  }
}

resource "aws_vpc_security_group_ingress_rule" "gateway_from_alb" {
  security_group_id            = aws_security_group.gateway.id
  description                  = "HTTP/WS from ALB"
  ip_protocol                  = "tcp"
  from_port                    = 8080
  to_port                      = 8080
  referenced_security_group_id = aws_security_group.alb.id
}

resource "aws_vpc_security_group_ingress_rule" "gateway_from_fanout" {
  security_group_id            = aws_security_group.gateway.id
  description                  = "gRPC DeliverMessage from Fanout"
  ip_protocol                  = "tcp"
  from_port                    = 8080
  to_port                      = 8080
  referenced_security_group_id = aws_security_group.fanout.id
}

resource "aws_vpc_security_group_egress_rule" "gateway_to_ingest" {
  security_group_id            = aws_security_group.gateway.id
  description                  = "gRPC PersistMessage to Ingest"
  ip_protocol                  = "tcp"
  from_port                    = 9091
  to_port                      = 9091
  referenced_security_group_id = aws_security_group.ingest.id
}

resource "aws_vpc_security_group_egress_rule" "gateway_to_redis" {
  security_group_id            = aws_security_group.gateway.id
  description                  = "Redis (presence, routing, revocation)"
  ip_protocol                  = "tcp"
  from_port                    = 6379
  to_port                      = 6379
  referenced_security_group_id = aws_security_group.redis.id
}

# -----------------------------------------------------------------------------
# sg-ingest — Ingest Service (Durability Plane)
# -----------------------------------------------------------------------------

resource "aws_security_group" "ingest" {
  name_prefix = "${local.name}-ingest-"
  description = "Ingest: inbound from Gateway, outbound to DynamoDB endpoint and MSK"
  vpc_id      = aws_vpc.main.id

  lifecycle {
    create_before_destroy = true
  }

  tags = {
    Name = "${local.name}-ingest"
  }
}

resource "aws_vpc_security_group_ingress_rule" "ingest_from_gateway" {
  security_group_id            = aws_security_group.ingest.id
  description                  = "gRPC PersistMessage from Gateway"
  ip_protocol                  = "tcp"
  from_port                    = 9091
  to_port                      = 9091
  referenced_security_group_id = aws_security_group.gateway.id
}

resource "aws_vpc_security_group_egress_rule" "ingest_to_dynamodb" {
  security_group_id = aws_security_group.ingest.id
  description       = "DynamoDB via VPC Gateway Endpoint"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  prefix_list_id    = data.aws_prefix_list.dynamodb.id
}

resource "aws_vpc_security_group_egress_rule" "ingest_to_msk" {
  security_group_id            = aws_security_group.ingest.id
  description                  = "Kafka produce to MSK"
  ip_protocol                  = "tcp"
  from_port                    = 9098
  to_port                      = 9098
  referenced_security_group_id = aws_security_group.msk.id
}

# -----------------------------------------------------------------------------
# sg-fanout — Fanout Worker (Fanout Plane)
# No inbound rules — pulls from Kafka, pushes to Redis and Gateway.
# -----------------------------------------------------------------------------

resource "aws_security_group" "fanout" {
  name_prefix = "${local.name}-fanout-"
  description = "Fanout: no inbound, outbound to MSK, Redis, and Gateway"
  vpc_id      = aws_vpc.main.id

  lifecycle {
    create_before_destroy = true
  }

  tags = {
    Name = "${local.name}-fanout"
  }
}

resource "aws_vpc_security_group_egress_rule" "fanout_to_msk" {
  security_group_id            = aws_security_group.fanout.id
  description                  = "Kafka consume from MSK"
  ip_protocol                  = "tcp"
  from_port                    = 9098
  to_port                      = 9098
  referenced_security_group_id = aws_security_group.msk.id
}

resource "aws_vpc_security_group_egress_rule" "fanout_to_redis" {
  security_group_id            = aws_security_group.fanout.id
  description                  = "Redis (routing, membership cache, pub/sub)"
  ip_protocol                  = "tcp"
  from_port                    = 6379
  to_port                      = 6379
  referenced_security_group_id = aws_security_group.redis.id
}

resource "aws_vpc_security_group_egress_rule" "fanout_to_gateway" {
  security_group_id            = aws_security_group.fanout.id
  description                  = "gRPC DeliverMessage to Gateway"
  ip_protocol                  = "tcp"
  from_port                    = 8080
  to_port                      = 8080
  referenced_security_group_id = aws_security_group.gateway.id
}

# -----------------------------------------------------------------------------
# sg-chatmgmt — Chat Management Service
# -----------------------------------------------------------------------------

resource "aws_security_group" "chatmgmt" {
  name_prefix = "${local.name}-chatmgmt-"
  description = "ChatMgmt: inbound from ALB, outbound to DynamoDB, MSK, and Redis"
  vpc_id      = aws_vpc.main.id

  lifecycle {
    create_before_destroy = true
  }

  tags = {
    Name = "${local.name}-chatmgmt"
  }
}

resource "aws_vpc_security_group_ingress_rule" "chatmgmt_from_alb" {
  security_group_id            = aws_security_group.chatmgmt.id
  description                  = "REST from ALB via grpc-gateway"
  ip_protocol                  = "tcp"
  from_port                    = 8083
  to_port                      = 8083
  referenced_security_group_id = aws_security_group.alb.id
}

resource "aws_vpc_security_group_egress_rule" "chatmgmt_to_dynamodb" {
  security_group_id = aws_security_group.chatmgmt.id
  description       = "DynamoDB via VPC Gateway Endpoint"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  prefix_list_id    = data.aws_prefix_list.dynamodb.id
}

resource "aws_vpc_security_group_egress_rule" "chatmgmt_to_msk" {
  security_group_id            = aws_security_group.chatmgmt.id
  description                  = "Kafka produce to MSK"
  ip_protocol                  = "tcp"
  from_port                    = 9098
  to_port                      = 9098
  referenced_security_group_id = aws_security_group.msk.id
}

resource "aws_vpc_security_group_egress_rule" "chatmgmt_to_redis" {
  security_group_id            = aws_security_group.chatmgmt.id
  description                  = "Redis (rate limiting, session revocation)"
  ip_protocol                  = "tcp"
  from_port                    = 6379
  to_port                      = 6379
  referenced_security_group_id = aws_security_group.redis.id
}

# -----------------------------------------------------------------------------
# sg-redis — ElastiCache Redis
# -----------------------------------------------------------------------------

resource "aws_security_group" "redis" {
  name_prefix = "${local.name}-redis-"
  description = "Redis: inbound from Gateway, Fanout, ChatMgmt on 6379"
  vpc_id      = aws_vpc.main.id

  lifecycle {
    create_before_destroy = true
  }

  tags = {
    Name = "${local.name}-redis"
  }
}

resource "aws_vpc_security_group_ingress_rule" "redis_from_gateway" {
  security_group_id            = aws_security_group.redis.id
  description                  = "Redis from Gateway"
  ip_protocol                  = "tcp"
  from_port                    = 6379
  to_port                      = 6379
  referenced_security_group_id = aws_security_group.gateway.id
}

resource "aws_vpc_security_group_ingress_rule" "redis_from_fanout" {
  security_group_id            = aws_security_group.redis.id
  description                  = "Redis from Fanout"
  ip_protocol                  = "tcp"
  from_port                    = 6379
  to_port                      = 6379
  referenced_security_group_id = aws_security_group.fanout.id
}

resource "aws_vpc_security_group_ingress_rule" "redis_from_chatmgmt" {
  security_group_id            = aws_security_group.redis.id
  description                  = "Redis from Chat Mgmt"
  ip_protocol                  = "tcp"
  from_port                    = 6379
  to_port                      = 6379
  referenced_security_group_id = aws_security_group.chatmgmt.id
}

# -----------------------------------------------------------------------------
# sg-msk — MSK (empty shell — populated in TF-2)
# -----------------------------------------------------------------------------

resource "aws_security_group" "msk" {
  name_prefix = "${local.name}-msk-"
  description = "MSK: empty shell — inbound rules added in TF-2 when MSK is provisioned"
  vpc_id      = aws_vpc.main.id

  lifecycle {
    create_before_destroy = true
  }

  tags = {
    Name = "${local.name}-msk"
  }
}
