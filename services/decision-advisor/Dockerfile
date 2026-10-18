FROM python:3.12.12-slim@sha256:f3fa41d74a768c2fce8016b98c191ae8c1bacd8f1152870a3f9f87d350920b7c
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1
WORKDIR /app
COPY backends.py server.py ./
RUN mkdir /models && chmod 0555 /models /app && chmod 0444 /app/*.py
USER 65532:65532
EXPOSE 8081
HEALTHCHECK --interval=30s --timeout=1s CMD ["python", "-c", "import http.client; c=http.client.HTTPConnection('127.0.0.1',8081,timeout=0.5); c.request('GET','/healthz'); assert c.getresponse().status == 200"]
ENTRYPOINT ["python", "server.py"]
CMD ["--host", "0.0.0.0", "--port", "8081"]
