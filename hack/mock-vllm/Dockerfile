FROM python:3.12-slim

WORKDIR /app
COPY mock_vllm.py /app/mock_vllm.py

EXPOSE 8000
CMD ["python3", "/app/mock_vllm.py"]
