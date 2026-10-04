# Releases

Fluxo inspirado em jira-mcp (GoReleaser), acari e tucupi (PR de preparação antes
da tag). Este projeto é Go: não usa Cargo, Node ou pnpm para release.

## Preparação do repositório

- Em Actions, permitir ao `GITHUB_TOKEN` criar pull requests. Se a organização
  não permitir, o mantenedor precisa ajustar essa política; não adicione PAT
  ou segredos de Woovi para contornar isso.
- Opcionalmente, proteger o environment `release` com revisão humana e restringir
  branches/tags. O YAML usa esse environment, mas não configura sua proteção.
- Definir/revisar licença, uso de marca e limitações das plataformas antes de
  tornar o primeiro draft público. O workflow não escolhe uma licença.
- Os workflows não usam AppID, conta Woovi ou credenciais de réplica.

## 1. Preparar a versão

No GitHub Actions, execute **Prepare Release** em `main`, informando uma versão
estável como `0.1.0` ou `v0.1.0`. Também é possível executar:

```sh
gh workflow run prepare-release.yml --ref main -f version=0.1.0
```

Esse comando é uma ação real, não um dry run: gera changelog convencional com
git-cliff 2.14.2 e abre `release/v0.1.0` → `main`. Não cria tags nem releases.
A versão precisa ser maior que todas as tags estáveis existentes; pré-releases,
leading zeros, espaços e conteúdo inválido são recusados. Nova execução reutiliza
um PR aberto, sem force-push. Depois do merge, não rode preparação de novo para
a mesma versão: revise o commit e prossiga para a tag.

O `GITHUB_TOKEN` não dispara automaticamente workflows de push/PR. Por isso a
preparação despacha explicitamente **CI** na branch de release, com permissão
`actions: write` apenas nesse job. Não há auto-merge.

## 2. Revisar e fazer merge

Confira o changelog gerado e aguarde CI: testes reais SQLite, múltiplos processos,
onboarding TTY, réplica Litestream/file/S3 compatível, race, formatação, vet,
lint, build, módulos e vulnerabilidades. CI também empacota as seis plataformas
com GoReleaser **em snapshot, sem publicar**, para detectar problemas antes da tag.

## 3. Criar a tag do commit revisado

Depois de fazer merge, atualize sua cópia e confira o commit antes de taguear:

```sh
git switch main
git pull --ff-only
git log -1 --oneline
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

Use a versão preparada, não copie `v0.1.0` para todas as releases. Tag/push são
ações explícitas do mantenedor. Não force nem mova uma tag já publicada.

**Release** exige tag estável `vX.Y.Z`, entrada correspondente no changelog e
commit pertencente ao histórico de `main`. Reexecuta o quality gate completo;
só depois usa GoReleaser 2.18.2 com permissão de escrita para criar um **draft**.
Nunca rode um teste end-to-end criando uma tag descartável no repositório real.

## 4. Conferir e publicar o draft

O draft contém:

- Binário `woovi-pix-mcp` para Linux, macOS e Windows, amd64/arm64.
- `.tar.gz` para Linux/macOS, `.zip` para Windows e arquivo-fonte `.tar.gz`.
- `checksums.txt` SHA-256, README, changelog e roteiro sandbox.
- Versão da tag/commit embutida no CLI (`--version`) e versão no handshake MCP.

Binários usam `CGO_ENABLED=0` e não precisam de Go/PostgreSQL/Docker em runtime.
Litestream não é incluído: instalação externa opcional. Os testes de runtime
são Linux; builds macOS/Windows não equivalem a teste funcional nesses sistemas.
O fallback de segredo por arquivo no Windows depende de validação de ACLs e
não é recomendado enquanto isso não for feito.

Baixe o artefato da sua plataforma e confira antes de executar. Exemplo Linux:

```sh
sha256sum --ignore-missing --check checksums.txt
tar -xzf woovi-pix-mcp_0.1.0_linux_amd64.tar.gz
./woovi-pix-mcp --version
```

macOS pode usar `shasum -a 256 -c checksums.txt` com os arquivos correspondentes;
Windows pode comparar `Get-FileHash -Algorithm SHA256` com a linha do arquivo.
Checksums protegem contra corrupção, não substituem a confiança na origem do
download. Os arquivos não possuem assinatura/notarização de plataforma.

Revise segurança, licença, changelog e artefatos antes de publicar o draft pela
interface do GitHub. Não existe publicação automática de pacotes, instalador,
Docker image, npm, Homebrew ou MCP Registry nesta implementação.

## Validação local sem publicar

Com GoReleaser 2.18.2, Python 3.12+, uv e golangci-lint 2.14.0 instalados:

```sh
make check
make release-snapshot
```

`dist/` é saída local ignorada pelo Git. `--snapshot` não cria tag, release ou
PR e não precisa de token GitHub. As ações de checkout/setup-go/GoReleaser e
git-cliff estão fixadas por SHA; versões dos executáveis também são fixadas.
Atualize os pins de forma revisada, deixando a CI validar a atualização.

## Legibilidade e lint

`make fmt` aplica gofmt/goimports e Ruff 0.16.10. `make check` também verifica
espaçamento entre blocos Go com `wsl_v5`, formato/lint Python e workflows com
actionlint 1.7.12. Assim, `gofmt` sozinho não é suficiente para aprovar código
com blocos lógicos colados. As correções de espaçamento não alteram os fluxos
financeiros, a persistência ou a recuperação.
