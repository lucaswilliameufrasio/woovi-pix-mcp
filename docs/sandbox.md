# Validação manual em sandbox

Este roteiro é para execução autorizada pelo operador. A implementação e a CI
não fizeram chamadas Woovi; resultados abaixo são critérios esperados, não
evidência de validação externa.

## Começar somente com consulta

Siga o [passo a passo no README](../README.md#teste-com-o-sandbox-da-própria-woovi):
acesso ao painel sandbox, obtenção do AppID e de uma cobrança de teste, setup,
doctor, instalação no cliente e primeira consulta. O simulador não precisa estar
rodando. `doctor` não chama a API; somente a consulta verifica a integração externa.

## Criação e idempotência opcionais

Faça estes passos **somente com autorização para escrita na conta sandbox**.
Não são necessários para testar consulta.

### 1. Criar um perfil com escrita habilitada

```sh
./bin/woovi-pix-mcp setup --profile sandbox-criacao
```

Responda:

- Ambiente: `sandbox`
- Conta: **o mesmo identificador do perfil sandbox de consulta**
- Habilitar criação: `yes` (opt-in)
- AppID: o AppID sandbox, com permissão de consulta e criação de cobranças

Se necessário, o fallback é `setup --profile sandbox-criacao --secret-file`,
somente se você aceitar armazenar o segredo sem criptografia em arquivo 0600.
Use outro nome se o perfil já existir; não mude a conta para fugir de idempotência.
Perfis da mesma conta/ambiente compartilham o banco SQLite.

### 2. Conferir e instalar no cliente

```sh
./bin/woovi-pix-mcp doctor --profile sandbox-criacao

./bin/woovi-pix-mcp install-mcp --profile sandbox-criacao \
  --client opencode --config ~/.config/opencode/opencode.json

./bin/woovi-pix-mcp install-mcp --profile sandbox-criacao \
  --client opencode --config ~/.config/opencode/opencode.json --apply
```

Esperado: ambiente `sandbox` e criação configurada como `true`. Reabra o cliente;
confirme a presença de `pix_create_charge` no servidor `woovi-pix-sandbox-criacao`.
Não selecione servidores de produção ou o perfil somente leitura.

### 3. Criar uma cobrança fictícia

Escolha uma referência inédita **para esse teste**, anote-a e mantenha o payload
idêntico em qualquer repetição. Exemplo de argumentos para `pix_create_charge`:

```json
{
  "reference": "teste-mcp-sandbox-001",
  "amount_cents": 100,
  "expires_in_seconds": 3600
}
```

Substitua a referência de exemplo se ela já tiver sido usada. O valor é R$ 1,00
fictício e a validade é uma hora. A faixa de expiração aceita é 300 a 2.592.000
segundos. Peça ao cliente para usar **apenas** `woovi-pix-sandbox-criacao`, sem
pagar o QR, transferir, reembolsar ou cancelar financeiramente.

Esperado em uma criação nova bem-sucedida: `charge` com referência/valor
correspondentes e `replayed: false`. Confira a cobrança no painel sandbox.

### 4. Repetir e verificar conflito

1. Repita **exatamente** o mesmo JSON: esperado o resultado persistido com
   `replayed: true`, sem uma segunda criação no provedor.
2. Mantenha referência e validade, mas altere `amount_cents` para `200`:
   esperado erro de conflito de idempotência, não outra cobrança.
3. Consulte a referência com `pix_get_charge`: esperado o valor original,
   `100` centavos, consistente com o painel.

Se ocorrer timeout ou outro resultado incerto, esses critérios de sucesso não
se aplicam até reconciliar. Não invente outra referência nem force reenvio:
consulte a referência no provedor. O MCP conserva operações não resolvidas;
`UNKNOWN` pode ser reconciliado por consulta sem nova criação. Ausência ou
divergência não significa autorização para repetir. Não apague o SQLite.

Ao terminar, encerre a sessão/servidor de criação ou desabilite sua entrada no
cliente e volte a usar o perfil somente leitura. Não use AppID/host de produção.

## Problemas frequentes

- **Cofre indisponível:** configure o cofre local ou escolha conscientemente
  `--secret-file`; não copie segredo para o cliente.
- **Perfil existente:** use outro nome; account/environment iguais compartilham
  o banco. Não mude o identificador de conta para contornar idempotência.
- **JSONC/config pública:** use configuração manual, preservando comentários,
  ou revise o arquivo JSON e permissões; o instalador não os reescreve à força.
- **Banco ocupado para recovery:** encerre MCP e réplica. Não remova locks
  enquanto processos rodam; locks do SO são liberados quando o processo termina.
- **Marcador recovered:** mantenha leitura até reconciliar o histórico externo.
- **Erro de réplica:** confira destino/permissões/cofre e instalação Litestream.
  Saída bruta é suprimida intencionalmente; logs manuais do Litestream podem
  conter informações sensíveis e não devem ser publicados sem sanitização.
