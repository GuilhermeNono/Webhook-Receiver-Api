# Rodando a API sem clonar o repositório

Esta pasta é auto-suficiente: quem só precisa **rodar** a API (não desenvolvê-la)
baixa só estes dois arquivos, sem precisar do código Go.

```bash
mkdir webhook-api && cd webhook-api
curl -O https://raw.githubusercontent.com/GuilhermeNono/Webhook-Receiver-Api/master/deploy/docker-compose.yml
curl -O https://raw.githubusercontent.com/GuilhermeNono/Webhook-Receiver-Api/master/deploy/.env.example
cp .env.example .env
```

Depois:

1. Preencha o `.env` com o **seu próprio** `NGROK_AUTHTOKEN` (conta grátis em
   [dashboard.ngrok.com](https://dashboard.ngrok.com) - o token não é
   compartilhável entre pessoas) e ajuste as demais variáveis se quiser.
2. Suba os containers:
   ```bash
   docker compose up
   ```
3. Para atualizar para a versão mais recente publicada no Docker Hub:
   ```bash
   docker compose pull && docker compose up -d
   ```

Só é preciso o `ngrok` aqui se o *seu* webhook precisar ser alcançável pela
internet (ex.: receber um evento de um serviço externo). Se você só vai
consumir a API localmente (ex.: um client rodando na mesma máquina), pode
remover o serviço `ngrok` do `docker-compose.yml` e falar direto com
`http://localhost:${API_PORT}`.
