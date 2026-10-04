# Woovi Pix MCP

Servidor MCP local em Go para consultar e, opcionalmente, criar cobranças Pix
Woovi. A ferramenta `pix_create_charge` fica desativada por padrão. Não há Pix
Out, transferências, reembolsos ou cancelamentos financeiros.

## Executar

```sh
WOOVI_API_BASE_URL=https://api.woovi-sandbox.com \
WOOVI_APP_ID='<AppID de sandbox>' \
go run ./cmd/woovi-pix-mcp
```

O AppID fica no processo servidor e nunca é um argumento ou resultado MCP. Logs
operacionais vão para stderr; stdout fica reservado ao protocolo stdio. O host
Woovi de produção é `https://api.woovi.com`, mas o servidor aceita apenas HTTPS
para hosts remotos.

Criação é uma capacidade explicitamente opt-in. Requer PostgreSQL, identificador
fixo da conta autorizada e limite máximo de R$ 100.000 por cobrança:

```sh
WOOVI_API_BASE_URL=https://api.woovi-sandbox.com \
WOOVI_APP_ID='<AppID de sandbox>' \
WOOVI_ENABLE_CHARGE_CREATION=true \
WOOVI_ACCOUNT_ID='<identificador interno da conta>' \
DATABASE_URL='postgres://...' \
go run ./cmd/woovi-pix-mcp
```

Cada referência é usada como `correlationID` Woovi e chave idempotente local.
Repetir o mesmo payload retorna resultado já persistido; outra carga para a
mesma referência conflita. Uma chamada com resultado incerto permanece `UNKNOWN`
e não é reenviada automaticamente: a ferramenta primeiro consulta a Woovi pela
referência e só fecha a operação quando referência e valor batem. Se não for
encontrada, permanece `UNKNOWN`. O valor explícito está limitado a R$ 100.000 e
expiração de 300 a 2.592.000 segundos. Tentativas e resultados são auditados no
PostgreSQL sem persistir AppID ou conteúdo do QR no log de auditoria. Esta fatia
não implementa approval workflow.

## Simulador local e teste MCP stdio

`examples/mcp-client.json` mostra o formato ilustrativo para clientes que usam
configuração MCP JSON. Troque o caminho pelo binário local e injete
`WOOVI_APP_ID` pelo gerenciador de segredos/ambiente do cliente; não copie um
AppID real para esse arquivo versionado. O placeholder `${WOOVI_APP_ID}` pode
precisar ser substituído conforme o cliente MCP usado.

O teste de integração sobe o simulador HTTP local e conecta um cliente MCP oficial
ao binário do servidor:

```sh
go test -count=1 ./...
```

Para executar o simulador manualmente, rode `go run ./cmd/woovi-simulator`; ele
escuta apenas em `127.0.0.1:8081` e fornece a cobrança `demo-charge` (AppID
`simulator`). Configure o servidor com `WOOVI_API_BASE_URL=http://127.0.0.1:8081`
e `WOOVI_APP_ID=simulator`. HTTP é permitido exclusivamente para localhost.

Exemplo de chamada: “consulte a cobrança `demo-charge`”. Para testar criação,
suba o simulador e habilite explicitamente escrita com PostgreSQL local conforme
a seção de configuração acima; use somente referências e valores fictícios.

## Contrato Woovi verificado

- Autenticação por `Authorization: <AppID>`; API retorna/aceita JSON e requer
  HTTPS. Limite documentado: 10 requisições por segundo.
- Consulta: `GET /api/v1/charge/{id}`; `id` aceita identificador da cobrança ou
  `correlationID`.
- O retorno MCP é minimizado a identificador, referência, estado, centavos BRL,
  expiração e código Pix; dados do pagador e payloads brutos são descartados.
- O cliente aplica limiter local conservador de 10 req/s (sem rajada) para
  respeitar o limite publicado pela Woovi.
- Referências: [Autenticação e limites](https://developers.woovi.com/en/docs/apis/api-getting-started),
  [API Redoc](https://developers.woovi.com/en/api-redoc),
  [Correlation ID/idempotência](https://developers.woovi.com/en/docs/concepts/correlation-id).

O simulador de teste também suporta POST e mantém cobranças idempotentes pela
referência em memória; nunca use credencial ou host de produção nos testes.

Com PostgreSQL local, os testes de persistência reais são ativados por:

```sh
TEST_DATABASE_URL='postgres://woovi:woovi-local-test@127.0.0.1:55463/woovi_test?sslmode=disable' \
go test -count=1 ./...
```
