# ALB module — Application Load Balancer, target groups, listeners, routing rules
#
# Implements TBD-TF0-5 (ALB & DNS Configuration).
# - HTTPS listener with 4 routing rules per ADR-014 §1.2
# - HTTP → HTTPS redirect
# - 3600s idle timeout for WebSocket connections (ADR-005)
# - No sticky sessions (ADR-014 §1.2)
# - 30s deregistration delay (Gateway drain budget)

locals {
  name = "${var.project_name}-${var.environment}"
}

# -----------------------------------------------------------------------------
# Application Load Balancer
# -----------------------------------------------------------------------------

resource "aws_lb" "main" {
  name               = "${local.name}-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [var.alb_security_group_id]
  subnets            = var.public_subnet_ids

  enable_deletion_protection = var.enable_deletion_protection

  connection_logs {
    enabled = false
  }

  idle_timeout = var.idle_timeout_seconds

  tags = {
    Name = "${local.name}-alb"
  }
}

# -----------------------------------------------------------------------------
# Target Groups
# -----------------------------------------------------------------------------

resource "aws_lb_target_group" "gateway" {
  name        = "${local.name}-gateway-tg"
  port        = var.gateway_port
  protocol    = "HTTP"
  vpc_id      = var.vpc_id
  target_type = "ip"

  deregistration_delay = var.deregistration_delay_seconds

  health_check {
    enabled             = true
    path                = var.health_check_path
    protocol            = "HTTP"
    port                = "traffic-port"
    healthy_threshold   = 2
    unhealthy_threshold = 3
    interval            = 15
    timeout             = 5
    matcher             = "200"
  }

  lifecycle {
    create_before_destroy = true
  }

  tags = {
    Name = "${local.name}-gateway-tg"
  }
}

resource "aws_lb_target_group" "chatmgmt" {
  name        = "${local.name}-chatmgmt-tg"
  port        = var.chatmgmt_port
  protocol    = "HTTP"
  vpc_id      = var.vpc_id
  target_type = "ip"

  deregistration_delay = var.deregistration_delay_seconds

  health_check {
    enabled             = true
    path                = var.health_check_path
    protocol            = "HTTP"
    port                = "traffic-port"
    healthy_threshold   = 2
    unhealthy_threshold = 3
    interval            = 15
    timeout             = 5
    matcher             = "200"
  }

  lifecycle {
    create_before_destroy = true
  }

  tags = {
    Name = "${local.name}-chatmgmt-tg"
  }
}

# -----------------------------------------------------------------------------
# HTTPS Listener (port 443) — default action: fixed 404
# Routing rules are defined below by priority.
# -----------------------------------------------------------------------------

resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.main.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = var.certificate_arn

  default_action {
    type = "fixed-response"

    fixed_response {
      content_type = "application/json"
      message_body = "{\"error\":\"not_found\"}"
      status_code  = "404"
    }
  }

  tags = {
    Name = "${local.name}-https"
  }
}

# -----------------------------------------------------------------------------
# HTTP Listener (port 80) — redirect to HTTPS
# All traffic encrypted in transit per ADR-013.
# -----------------------------------------------------------------------------

resource "aws_lb_listener" "http_redirect" {
  load_balancer_arn = aws_lb.main.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "redirect"

    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }

  tags = {
    Name = "${local.name}-http-redirect"
  }
}

# -----------------------------------------------------------------------------
# Listener Rules (priority order per TBD-TF0-5)
#
# Priority 1: /ws with WebSocket upgrade → Gateway
# Priority 2: /api/v1/* → Chat Mgmt
# Priority 3: /health → Gateway
# Default: 404 (set on listener above)
# -----------------------------------------------------------------------------

resource "aws_lb_listener_rule" "websocket" {
  listener_arn = aws_lb_listener.https.arn
  priority     = 1

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.gateway.arn
  }

  condition {
    path_pattern {
      values = ["/ws", "/ws/*"]
    }
  }

  condition {
    http_header {
      http_header_name = "Upgrade"
      values           = ["websocket"]
    }
  }

  tags = {
    Name = "${local.name}-ws-rule"
  }
}

resource "aws_lb_listener_rule" "api" {
  listener_arn = aws_lb_listener.https.arn
  priority     = 2

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.chatmgmt.arn
  }

  condition {
    path_pattern {
      values = ["/api/v1/*"]
    }
  }

  tags = {
    Name = "${local.name}-api-rule"
  }
}

resource "aws_lb_listener_rule" "health" {
  listener_arn = aws_lb_listener.https.arn
  priority     = 3

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.gateway.arn
  }

  condition {
    path_pattern {
      values = ["/health"]
    }
  }

  tags = {
    Name = "${local.name}-health-rule"
  }
}
