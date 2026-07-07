# 🪝 Webhook Receiver API

Uma API simples e dinâmica em **Go** para receber webhooks, feita para rodar em **Docker** com um túnel público automático via **ngrok**. 🚀

## 📌 O que é?

O **Webhook Receiver API** é um servidor HTTP minimalista que expõe um endpoint para receber requisições `POST` (webhooks), lê o corpo da requisição, faz o parse do JSON recebido e responde confirmando o recebimento.

É útil para:
- 🧪 Testar integrações que enviam webhooks (pagamentos, CI/CD, chatbots, etc)
- 🌐 Expor rapidamente um endpoint local para a internet usando o `ngrok`
- 📝 Auditar/depurar payloads recebidos, com histórico consultável (paginado ou em tempo real) e log opcional no terminal

## 🏗️ Como funciona

- O serviço `webhook-api` é buildado via Dockerfile multi-stage (Go → Alpine) e escuta na porta configurada.
- O serviço `ngrok` cria um túnel público apontando para o `webhook-api`, expondo o endpoint na internet.
- Toda a configuração é feita através de um arquivo `.env`, sem necessidade de alterar código ou o `docker-compose.yml`.

## ⚙️ Variáveis de ambiente

Copie o arquivo [.env.example](.env.example) para `.env` e ajuste os valores conforme necessário:

```bash
cp .env.example .env
```

| Variável | Descrição | Padrão |
|---|---|---|
| 🔑 `NGROK_AUTHTOKEN` | Chave de autenticação para utilizar o ngrok. Obtenha a sua em [dashboard.ngrok.com](https://dashboard.ngrok.com) | *(obrigatório)* |
| 🎯 `WEBHOOK_ENDPOINT` | Endpoint (rota) que será utilizado para receber os webhooks | `/webhook` |
| 🔌 `API_PORT` | Porta em que a API vai escutar | `8080` |
| 🖥️ `LOG_IN_BASH` | Se `true`, os logs de cada webhook recebido também aparecem no terminal (a persistência em `data/webhook.db` acontece sempre, independente desta flag) | `true` |
| 🛠️ `ADMIN_ROUTE` | Prefixo das rotas administrativas (config + logs paginados) | `/admin` |
| 🔐 `ADMIN_TOKEN` | Token exigido (header `X-Admin-Token`) para acessar as rotas administrativas. Se vazio, um token aleatório é gerado a cada start e impresso no log | *(vazio)* |

> ⚠️ **Importante:** mesmo com `LOG_IN_BASH=false`, a mensagem inicial informando que a API subiu (`listening on ...`) **sempre** aparece no terminal. Apenas os logs de cada webhook recebido é que respeitam essa flag.

## 🚀 Como rodar

1. Crie o seu `.env` a partir do exemplo:
   ```bash
   cp .env.example .env
   ```
2. Preencha o `NGROK_AUTHTOKEN` e ajuste as demais variáveis se quiser.
3. Suba os containers:
   ```bash
   docker compose up --build
   ```
4. Sua API estará disponível em `http://localhost:${API_PORT}${WEBHOOK_ENDPOINT}` 🎉
5. A URL pública gerada pelo ngrok pode ser consultada no painel local: [http://localhost:4040](http://localhost:4040) 🌐

## 📨 Testando o webhook

```bash
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{"evento": "teste"}'
```

Resposta esperada:

```json
{
  "status": "received",
  "payload": { "evento": "teste" }
}
```

## 📁 Logs

- 🖥️ **Terminal (`docker logs webhook-api`)**: exibe a mensagem de inicialização sempre, e os logs de webhook apenas se `LOG_IN_BASH=true`.
- 🗄️ **Banco local (`data/webhook.db`)**: toda requisição recebida no webhook (headers, corpo, host, IP remoto, método, path e query) é persistida em um banco SQLite local (modo WAL, focado em performance), sempre, independente da flag acima. Consulte via [`GET /admin/logs`](#-endpoints-administrativos) ou em [tempo real via SSE](#-acompanhar-logs-em-tempo-real-sse--get-adminlogsstream).

O arquivo vive dentro da pasta `data/`, que é montada como volume (`./data:/app/data`) e fica disponível também na raiz do projeto no host. É montada a pasta inteira (não o arquivo individualmente) de propósito: se o bind mount apontasse direto para um arquivo que ainda não existe no host, o Docker criaria uma **pasta** no lugar dele (e o app quebraria ao tentar abrir "arquivo" que na verdade é um diretório). Montando o diretório, o próprio binário cria o arquivo dentro dele na primeira execução.

## 🛠️ Endpoints administrativos

Todas as rotas abaixo ficam sob o prefixo definido em `ADMIN_ROUTE` (padrão `/admin`) e exigem o header `X-Admin-Token` com o valor de `ADMIN_TOKEN` (ou o token aleatório impresso no log de inicialização, se a variável não tiver sido definida).

### Alterar porta/endpoint do webhook — `GET|POST /admin/config`

- `GET`: retorna a porta e o endpoint atualmente em uso pelo processo em execução.
- `POST`/`PUT`: recebe `{"port": "9090", "webhook_endpoint": "/novo-webhook"}` (ambos os campos são opcionais, mas ao menos um é obrigatório) e grava os novos valores no arquivo `.env`.

  ⚠️ **A alteração só é persistida no `.env` — é necessário reiniciar a aplicação (`docker compose up --build` ou similar) para que ela entre em vigor.**

```bash
curl -X POST http://localhost:8080/admin/config \
  -H "Content-Type: application/json" \
  -H "X-Admin-Token: SEU_TOKEN" \
  -d '{"port": "9090", "webhook_endpoint": "/novo-webhook"}'
```

### Consultar logs paginados — `GET /admin/logs`

Parâmetros de query opcionais: `page` (padrão `1`) e `limit` (padrão `20`, máximo `100`).

```bash
curl "http://localhost:8080/admin/logs?page=1&limit=20" \
  -H "X-Admin-Token: SEU_TOKEN"
```

### Acompanhar logs em tempo real (SSE) — `GET /admin/logs/stream`

Em vez de ficar chamando `/admin/logs` periodicamente, é possível abrir uma conexão via **Server-Sent Events**: a resposta fica aberta e cada novo webhook recebido é enviado assim que é persistido no banco.

```bash
curl -N "http://localhost:8080/admin/logs/stream" \
  -H "X-Admin-Token: SEU_TOKEN"
```

Cada evento chega no formato:

```
id: 42
event: log
data: {"id":42,"received_at":"...","method":"POST","path":"/webhook", ...}
```

- A cada ~15s, um comentário `: heartbeat` é enviado para manter a conexão viva através de proxies/túneis (como o ngrok) que fecham conexões ociosas.
- **Reconexão sem perder eventos**: o campo `id` de cada evento é o mesmo `Last-Event-ID` que o `EventSource` do browser reenvia automaticamente ao reconectar. O servidor usa esse valor para reenviar exatamente os eventos perdidos, em vez do histórico todo. Um client sem esse comportamento nativo (ex.: `curl`) pode simular isso com `?last_event_id=42`.
- ⚠️ A API nativa `EventSource` do browser **não permite enviar headers customizados**, então não dá pra passar `X-Admin-Token` direto com `new EventSource(url)`. Para consumir de um browser, use uma lib baseada em `fetch` (ex.: `@microsoft/fetch-event-source`) que suporte headers, mantendo a autenticação consistente com as demais rotas administrativas.

## 🔒 Segurança

- Rotas administrativas exigem token (`X-Admin-Token`), comparado em tempo constante.
- Headers de resposta com `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Content-Security-Policy: default-src 'none'`, `Referrer-Policy: no-referrer` e `Cache-Control: no-store`.
- Tamanho do corpo da requisição limitado (5MB no webhook, 64KB nas rotas de config) para mitigar payloads abusivos.
- Rotas e portas informadas em `/admin/config` são validadas (charset restrito, sem `..`/`//`, porta entre 1-65535) antes de serem persistidas.
- Toda query ao SQLite usa parâmetros (nunca concatenação de string), prevenindo SQL injection.

## 🗂️ Estrutura do projeto

```
.
├── main.go              # Entrypoint e wiring do servidor
├── config.go             # Leitura/validação de env vars e persistência no .env
├── security.go            # Headers de segurança, auth do admin, guarda de métodos
├── storage.go              # Camada SQLite (WAL) para os logs de webhook
├── hub.go                   # Pub/sub em memória para distribuir logs aos clientes SSE
├── stream.go                 # Handler SSE (GET /admin/logs/stream)
├── handlers.go                 # Handlers HTTP (webhook, config, logs)
├── go.mod / go.sum          # Módulo Go e dependências
├── Dockerfile               # Build multi-stage (build Go + runtime Alpine)
├── docker-compose.yml       # Orquestração da API + ngrok
├── .env.example              # Modelo das variáveis de ambiente
└── data/                      # Gerado em runtime (montado como volume; fora do git)
    └── webhook.db              # Banco SQLite com o histórico de requisições
```

## 🛠️ Tecnologias

- 🐹 **Go** — servidor HTTP
- 🐳 **Docker / Docker Compose** — containerização e orquestração
- 🌍 **ngrok** — exposição pública do endpoint local

## 📄 Licença

Este projeto está sob a licença [MIT](LICENSE). 🎉
