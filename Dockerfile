FROM nginx

COPY devops/prometheus-network-check ./
CMD ./prometheus-network-check
EXPOSE 8010
