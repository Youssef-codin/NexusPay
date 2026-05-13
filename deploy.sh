#!/bin/bash
set -e

echo "Building image..."
docker build -t ghcr.io/youssef-codin/nexuspay-backend:latest .

echo "Pushing to ghcr..."
docker push ghcr.io/youssef-codin/nexuspay-backend:latest

echo "Deploying to EC2..."
ssh -i ~/Documents/Work/nexuspay/prod.pem ubuntu@100.107.4.62 "cd ~/nexuspay && docker compose pull && docker compose up -d"

echo "Done!"
