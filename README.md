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

## Licença

Copyright 2026 Lucas Eufrasio e contribuidores.
Licenciado sob a [Apache License 2.0](LICENSE).

Projeto independente, não oficial da Woovi. A licença cobre o código deste
repositório, não concede direitos sobre marcas de terceiros e não substitui
os termos de uso da API Woovi. Dependências mantêm suas próprias licenças.

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

## Instalar

No Linux ou macOS, instale a release mais recente em `~/.local/bin`:

```sh
curl -fsSL https://github.com/lucaswilliameufrasio/woovi-pix-mcp/releases/latest/download/woovi-pix-mcp-installer.sh | sh
```

Para instalar uma versão específica, baixe o instalador da release e informe a
tag. Exemplo com `v0.1.2`:

```sh
curl -fsSL https://github.com/lucaswilliameufrasio/woovi-pix-mcp/releases/download/v0.1.2/woovi-pix-mcp-installer.sh | sh -s -- --tag v0.1.2
```

No Windows PowerShell, para a release mais recente:

```powershell
Invoke-WebRequest https://github.com/lucaswilliameufrasio/woovi-pix-mcp/releases/latest/download/woovi-pix-mcp-installer.ps1 -OutFile woovi-pix-mcp-installer.ps1
.\woovi-pix-mcp-installer.ps1
```

Passe `-Tag v0.1.2` para fixar uma versão no Windows.
Os instaladores verificam o SHA-256 do pacote antes da instalação. É necessário
adicionar `~/.local/bin` ao `PATH` se ainda não estiver configurado.

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

Para `make fmt`/`make check`, além de Go, instale golangci-lint 2.14.0, Python
3.12+ e uv. Ruff é resolvido na versão fixa 0.16.10. O lint também verifica
espaçamento entre blocos Go; não basta passar no `gofmt`.

```sh
make test
make check
make migrate-create name=add_charge_metadata
```

Os testes usam arquivos SQLite reais temporários, inclusive processos separados.
As novas migrations são timestamped e criadas pela
Goose CLI. Não há target `migrate-down`/`reset`: a migration Down remove as
tabelas de operação/auditoria e pode apagar evidência de idempotência.

## Processo de release

**Prepare Release** abre um PR de changelog; após revisão/CI e merge, uma tag
`vX.Y.Z` dispara **Release**, que repete os checks e cria um **draft** com binários
Linux/macOS/Windows (amd64/arm64) e checksums SHA-256. Publicar o draft continua
sendo uma decisão manual. Nenhuma release é disparada ao adicionar os workflows.

Veja [docs/releases.md](docs/releases.md) para configuração, preparação,
tagueamento, verificação de artefatos e limitações de plataforma. Para validar
o empacotamento local sem publicar: `make release-snapshot` (GoReleaser 2.18.2).

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

## Teste rápido local — sem chamar a Woovi

Execute os comandos na raiz deste repositório, com Go 1.27.1 instalado. Este
roteiro usa somente dados fictícios e não precisa de conta Woovi ou Docker.

### 1. Terminal 1: subir o simulador

```sh
go run ./cmd/woovi-simulator
```

Deixe esse terminal aberto. O simulador escuta em `127.0.0.1:8081`.

### 2. Terminal 2: compilar e configurar

```sh
go build -o ./bin/woovi-pix-mcp ./cmd/woovi-pix-mcp
./bin/woovi-pix-mcp setup --profile teste
```

Responda ao assistente:

- Ambiente: `simulator`
- Conta: `conta-teste`
- Habilitar criação: `no`
- AppID: `simulator` (a entrada fica oculta)

Se o cofre do sistema estiver indisponível, repita o setup com o fallback
explícito abaixo. Ele guarda o AppID sem criptografia em arquivo privado 0600:

```sh
./bin/woovi-pix-mcp setup --profile teste --secret-file
```

Se o perfil já existir, reutilize-o ou escolha outro nome; não há sobrescrita.

### 3. Conferir a configuração

```sh
./bin/woovi-pix-mcp doctor --profile teste
```

**Esperado:** ambiente `simulator`, credencial disponível sem exibir AppID e
criação desabilitada. O banco pode aparecer como não inicializado: isso é normal
em modo somente leitura. `doctor` não consulta nem mesmo o simulador.

### 4. Conectar ao OpenCode V2

Primeiro valide a alteração (preview), depois aplique:

```sh
./bin/woovi-pix-mcp install-mcp --profile teste \
  --client opencode --config ~/.config/opencode/opencode.json

./bin/woovi-pix-mcp install-mcp --profile teste \
  --client opencode --config ~/.config/opencode/opencode.json --apply
```

Use o caminho do arquivo realmente usado pelo seu cliente. Precisa ser JSON,
não JSONC; diretório 0700 e arquivo 0600, sem symlinks. Se o instalador recusar,
revise o motivo antes de alterar permissões ou use configuração manual. Para
Claude/Cursor, selecione `--client claude`/`--client cursor` e o respectivo arquivo.
`examples/mcp-client.json` ilustra a configuração manual: binário e perfil,
**nunca AppID**. O binário precisa permanecer no caminho usado na instalação.

### 5. Consultar a cobrança fictícia

Reabra o OpenCode para carregar o servidor `woovi-pix-teste` e peça:

> Use o servidor woovi-pix-teste para consultar a cobrança `demo-charge` com
> `pix_get_charge`. Não use outros servidores nem crie cobranças.

**Esperado:** ID `demo-charge`, referência `demo-order-001`, estado `ACTIVE` e
valor `1250` centavos (**R$ 12,50**), com código Pix fictício. Nenhuma chamada
Woovi ou operação financeira real é feita. Termine o simulador com Ctrl+C.

**O simulador manual só suporta consulta.** Criação/idempotência são exercitadas
pelo simulador específico dos testes automatizados, não por esse comando.
Para rodar a suíte e as verificações do projeto:

```sh
make check
```

## Teste com o sandbox da própria Woovi

Este roteiro usa a API de teste externa da Woovi, **não o simulador local**.
Não é necessário executar `woovi-simulator`. Execute apenas com autorização
para usar essa conta; os passos abaixo não foram executados pela implementação
ou pela CI. Não use credenciais de produção nem faça pagamentos reais.

### 1. Preparar a conta e uma cobrança de teste

- Acesse [app.woovi-sandbox.com](https://app.woovi-sandbox.com/). O sandbox tem
  cadastro separado: dados de produção não funcionam nele.
- No painel **do sandbox**, obtenha um AppID da sua conta. A documentação de
  autenticação indica `Admin Panel > Permissions > APIs` para gerar a chave,
  com permissão de administrador. Use os escopos necessários para consulta.
- Tenha uma cobrança criada no sandbox pelo painel e copie seu ID ou
  `correlationID`. `demo-charge` é exclusivo do simulador e não deve ser usado aqui.

Referências oficiais: [ambiente de teste](https://developers.woovi.com/docs/test-environment)
e [criação da chave API](https://developers.woovi.com/en/docs/apis/api-getting-started).

### 2. Compilar e configurar o perfil sandbox

```sh
go build -o ./bin/woovi-pix-mcp ./cmd/woovi-pix-mcp
./bin/woovi-pix-mcp setup --profile sandbox
```

Responda:

- Ambiente: `sandbox` (seleciona `https://api.woovi-sandbox.com`)
- Conta: um identificador estável dessa conta, por exemplo `minha-conta-sandbox`
- Habilitar criação: `no`, para começar somente com consulta
- AppID: **o AppID do sandbox**, no prompt oculto; nunca cole no chat ou no cliente

A conta é o escopo local de idempotência, não outro segredo; mantenha o mesmo
identificador ao criar perfis para essa conta. Se o cofre estiver indisponível e
você aceitar o arquivo privado sem criptografia, use explicitamente:

```sh
./bin/woovi-pix-mcp setup --profile sandbox --secret-file
```

### 3. Conferir e conectar ao cliente

```sh
./bin/woovi-pix-mcp doctor --profile sandbox

./bin/woovi-pix-mcp install-mcp --profile sandbox \
  --client opencode --config ~/.config/opencode/opencode.json

./bin/woovi-pix-mcp install-mcp --profile sandbox \
  --client opencode --config ~/.config/opencode/opencode.json --apply
```

Valem os requisitos de JSON e permissões do roteiro local. **Esperado no doctor:**
ambiente `sandbox`, credencial disponível e criação desabilitada. Isso valida
apenas a configuração local, **não autenticação ou conectividade com a Woovi**.

### 4. Consultar uma cobrança do sandbox

Reabra o cliente e substitua `<ID_OU_CORRELATION_ID>` pelo identificador copiado
do painel. Se houver outros perfis instalados, selecione `woovi-pix-sandbox`:

> Use somente o servidor woovi-pix-sandbox para chamar `pix_get_charge` com
> `id` igual a `<ID_OU_CORRELATION_ID>`. Não crie nem pague cobranças.

**Esperado:** a cobrança da sua conta sandbox, com ID/referência, estado e valor
em centavos coerentes com o painel, sem AppID ou dados do pagador. Estado e
valor dependem da cobrança escolhida; não há resultado fixo de R$ 12,50 aqui.
Essa consulta é a verificação efetiva de API, credencial e escopo da conta.

### 5. Opcional: testar criação e idempotência no sandbox

Somente se quiser testar escrita e tiver autorização, siga o roteiro detalhado
em [docs/sandbox.md](docs/sandbox.md#criação-e-idempotência-opcionais). Ele cria
outro perfil com opt-in explícito e explica como repetir o mesmo payload e
verificar conflito. Não pague o QR gerado, não use produção e não invente outra
referência para contornar um timeout ou resultado incerto.

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
