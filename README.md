# Woovi Pix MCP

Servidor MCP local em Go para consultar e, opcionalmente, criar cobranças Pix
Woovi. A ferramenta `pix_create_charge` fica desativada por padrão. Não há Pix
Out, transferências, reembolsos ou cancelamentos financeiros.

Stack: Go 1.27.1, MCP Go SDK v1.8.0, SQLite embutido e Goose v3 para migrações
SQL. Quando escrita está habilitada, o servidor cria o banco privado e aplica
as migrações ao iniciar. Não requer PostgreSQL nem Docker.

CLI local: setup, perfis, stdio, doctor, instalação segura em clientes JSON e
Litestream opcional. Transporte remoto, distribuição pública e operações Pix
Out estão fora do escopo. Validação externa no sandbox Woovi ainda não realizada.

## Configurar pela CLI

```sh
go build -o ./bin/woovi-pix-mcp ./cmd/woovi-pix-mcp
./bin/woovi-pix-mcp setup --profile sandbox
./bin/woovi-pix-mcp doctor --profile sandbox
```

O assistente pede ambiente, identificador estável da conta e opt-in de criação.
O AppID é digitado sem eco e salvo no cofre de credenciais do sistema. Se o
cofre não estiver disponível, o setup falha: não há fallback silencioso. Use
`setup --profile sandbox --secret-file` somente se aceitar guardar o segredo
sem criptografia em arquivo restrito ao usuário. Permissões POSIX são validadas;
ACLs Windows ainda precisam validação antes de recomendar esse fallback lá.
Perfis existentes não são sobrescritos: use outro nome para nova configuração.

Configure seu cliente com o caminho absoluto do binário e argumentos
`["stdio", "--profile", "sandbox"]`. Não inclua AppID na configuração do cliente.
`doctor` é diagnóstico local; não chama Woovi nem cria cobrança.

`profiles` lista os nomes configurados sem carregar ou exibir credenciais.
O identificador de conta precisa ser estável e corresponder à conta do AppID:
o servidor não consegue verificar isso offline. Dois perfis da mesma conta e
ambiente compartilham o histórico de idempotência, inclusive após trocar AppID.

## Instalar no cliente MCP

Selecione explicitamente cliente e arquivo. O padrão só valida a alteração;
`--apply` salva com backup privado, preservando entradas alheias. Exemplo:

```sh
./bin/woovi-pix-mcp install-mcp --profile sandbox --client opencode --config ~/.config/opencode/opencode.json
./bin/woovi-pix-mcp install-mcp --profile sandbox --client opencode --config ~/.config/opencode/opencode.json --apply
```

Clientes aceitos: `opencode` **V2** (`mcp.servers`), `claude` e `cursor`
(`mcpServers`). O caminho é sempre explícito: não há descoberta ou edição
automática de todos os clientes. JSONC, arquivos públicos ou symlinks são
recusados; revise permissões antes de aplicar (diretório 0700, arquivo 0600).
Uma entrada diferente com o mesmo nome não é sobrescrita. Uma entrada idêntica
não gera nova alteração/backup. Codex/TOML deve ser configurado manualmente.

## Comandos de desenvolvimento

`make help` lista os comandos disponíveis. Exemplos:

```sh
make test
make check
make migrate-create name=add_charge_metadata
```

Os testes usam arquivos SQLite reais temporários, inclusive processos separados.
As novas migrations são timestamped e criadas pela
Goose CLI. Não há target `migrate-down`/`reset`: a migration Down remove as
tabelas de operação/auditoria e pode apagar evidência de idempotência.

## Executar

```sh
./bin/woovi-pix-mcp stdio --profile sandbox
```

Para compilar um binário local sem instalá-lo globalmente:

```sh
go build -o ./bin/woovi-pix-mcp ./cmd/woovi-pix-mcp
```

Execute o binário com `stdio --profile sandbox`. O diretório
`bin/` é apenas uma saída local e não deve ser versionado.

O AppID fica no processo servidor e nunca é um argumento ou resultado MCP. Logs
operacionais vão para stderr; stdout fica reservado ao protocolo stdio. O host
Woovi de produção é `https://api.woovi.com`, mas o servidor aceita apenas HTTPS
para hosts remotos.

Criação é uma capacidade explicitamente opt-in. Requer identificador fixo da
conta autorizada e limite máximo de R$ 100.000 por cobrança, sem banco externo:

No `setup`, responda `yes` somente se quiser habilitar criação. Para automação,
o modo legado sem subcomando ainda aceita `WOOVI_API_BASE_URL`, `WOOVI_APP_ID`,
`WOOVI_ACCOUNT_ID` e `WOOVI_ENABLE_CHARGE_CREATION`; injete o segredo pelo cofre
da automação, nunca em argv, histórico do shell ou configuração dos clientes.

Cada referência é usada como `correlationID` Woovi e chave idempotente local.
Repetir o mesmo payload retorna resultado já persistido; outra carga para a
mesma referência conflita. Uma chamada com resultado incerto permanece `UNKNOWN`
e não é reenviada automaticamente: a ferramenta primeiro consulta a Woovi pela
referência e só fecha a operação quando referência e valor batem. Se não for
encontrada, permanece `UNKNOWN`. O valor explícito está limitado a R$ 100.000 e
expiração de 300 a 2.592.000 segundos. Tentativas e resultados são auditados no
SQLite sem persistir AppID ou conteúdo do QR no log de auditoria. Esta fatia
não implementa approval workflow.

O arquivo SQLite é criado sob o diretório de configuração do usuário, em
`woovi-pix-mcp/state/<escopo>/operations.db`; o escopo separa URL/ambiente e conta.
`WOOVI_DATABASE_PATH` permite indicar outro arquivo privado. Nunca apague o
arquivo para resolver um erro: ele guarda a evidência das tentativas anteriores.
SQLite usa WAL, synchronous FULL e espera limitada para escritores concorrentes.
Não há DATABASE_URL nem importação PostgreSQL; esse MCP não teve usuários legados.

## Simulador local e teste MCP stdio

`examples/mcp-client.json` mostra o formato ilustrativo para clientes que usam
configuração MCP JSON. Troque o caminho pelo binário local; somente o nome do
perfil aparece no cliente, nunca o AppID.

O teste de integração sobe o simulador HTTP local e conecta um cliente MCP oficial
ao binário do servidor:

```sh
go test -count=1 ./...
```

Para executar o simulador manualmente, rode `go run ./cmd/woovi-simulator`; ele
escuta apenas em `127.0.0.1:8081` e fornece a cobrança `demo-charge` (AppID
`simulator`). Faça setup de um perfil `simulator`, escolhendo esse ambiente e
digitando `simulator` no prompt AppID. HTTP é permitido exclusivamente para localhost.

Exemplo de chamada: “consulte a cobrança `demo-charge`”. Para testar criação,
suba o simulador e habilite explicitamente escrita com SQLite local conforme
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

Todos os testes de persistência SQLite executam automaticamente em `make test`,
sem credenciais ou infraestrutura externa.

## Litestream opcional e recuperação

Instale separadamente [Litestream 0.5.12+ (série 0.5.x)](https://litestream.io/install/), validado
com 0.5.17. Não é iniciado automaticamente e o MCP funciona sem ele.

```sh
./bin/woovi-pix-mcp replica-setup --profile sandbox --replica-url file:///absolute/private/backup/sandbox
# Alternativa: S3 ou compatível; access key e secret key entram em prompts sem eco.
./bin/woovi-pix-mcp replica-setup --profile sandbox --replica-url s3://my-bucket/account-specific-prefix --region us-east-1
./bin/woovi-pix-mcp replica-run --profile sandbox
```

Use apenas um dos destinos; configuração existente não é sobrescrita. Para S3
compatível use `--endpoint https://...`. Credenciais usam cofre do sistema ou
`--secret-file` explicitamente; não coloque credenciais na URL. O processo filho
recebe somente as credenciais da réplica, não o AppID. Prefira discos privados e
criptografia/controle de acesso do bucket: a réplica contém dados de cobranças.

Inicie stdio uma vez para inicializar o banco antes da primeira réplica.
`replica-run` fica em foreground; Ctrl+C o encerra. Um lock impede duas réplicas
gerenciadas para o mesmo banco. `replica-status` verifica o estado local via
Litestream; **não atesta sincronização remota nem atraso zero**. Saída bruta do
processo externo é suprimida para evitar vazamento em mensagens de erro.

Para recuperar: pare todos os processos MCP e réplica; arquive manualmente o
banco e seus sidecars, sem excluí-los. O destino e sidecars precisam estar
ausentes; não existe opção force/overwrite nesta CLI.

```sh
./bin/woovi-pix-mcp replica-restore --profile sandbox --acknowledge-stale-state
./bin/woovi-pix-mcp doctor --profile sandbox
```

A restauração faz integrity check completo e cria `operations.db.recovered`,
que bloqueia inicialização com criação habilitada. **Não remova esse marcador
antes de conferir o histórico da Woovi e reconciliar operações possivelmente
ausentes na cópia**. Se não conseguir reconciliar, permaneça em leitura. Não há
importação automática de histórico PSP nem botão de desbloqueio automático.
Uma falha de restore pode deixar arquivos parciais: arquive-os e investigue,
não reinicie com estado vazio. O marcador também permanece em caso de falha.

Litestream replica de forma assíncrona: RPO depende do último envio confirmado;
RTO depende da recuperação manual. Não oferece HA automática ou multiwriter.
Uma cópia antiga pode esquecer uma criação já efetivada no provedor; restaurar
o banco não autoriza repetir pedidos financeiros com novas referências.

Integração real de réplica em arquivo e armazenamento S3 local (SeaweedFS):

```sh
TEST_LITESTREAM_BINARY=/absolute/path/litestream make test-litestream
```

Esse target precisa Docker somente para os testes opcionais de réplica; uso
normal continua sem Docker. CI executa ambos os backends e valida o checksum
do binário fixado. Não chama Woovi nem usa credenciais reais.

Roteiro e troubleshooting: [docs/sandbox.md](docs/sandbox.md).
