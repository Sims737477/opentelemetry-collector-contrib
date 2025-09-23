docker rm -f $(docker ps -aq)
docker compose up -d
docker logs -f opentelemetry-collector
