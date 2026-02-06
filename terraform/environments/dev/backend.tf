# Remote state backend — S3 + DynamoDB per TBD-TF0-8.
# Bootstrap with: scripts/bootstrap-terraform-state.sh

terraform {
  backend "s3" {
    bucket         = "messaging-platform-terraform-state"
    key            = "dev/terraform.tfstate"
    region         = "us-east-2"
    dynamodb_table = "terraform-locks"
    encrypt        = true
  }
}
