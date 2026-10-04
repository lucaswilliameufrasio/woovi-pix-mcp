# Validação manual em sandbox

Este roteiro é para execução autorizada pelo operador. A implementação e a CI
não fizeram chamadas Woovi; resultados abaixo são critérios esperados, não
evidência de validação externa.

1. Crie um perfil pelo `setup --profile sandbox` usando o AppID **do sandbox**
   e um identificador estável dessa conta. Comece sem habilitar criação.
2. Execute `doctor --profile sandbox`: credencial disponível, ambiente sandbox,
   criação desabilitada e nenhum pedido ao provedor.
3. Instale explicitamente no cliente com `install-mcp` (preview, depois apply).
   Confirme que só o caminho do binário e o nome do perfil aparecem na entrada.
4. Consulte uma cobrança de teste existente com `pix_get_charge`. Esperado:
   referência/ID, estado, centavos BRL e código Pix; nenhum AppID ou dado do pagador.
5. Apenas com autorização específica para criar cobranças fictícias, configure
   um novo perfil da mesma conta com criação opt-in. Use uma referência inédita,
   valor fictício pequeno e expiração válida (300 a 2.592.000 segundos).
6. Repita a chamada com a mesma referência e payload: esperado mesmo resultado,
   sem novo POST. Altere o valor mantendo referência: esperado conflito local.
7. Se houver timeout, não gere outra referência nem force retry: consulte a
   referência no provedor. O MCP mantém o resultado incerto e reconcilia sem
   nova criação; ausência ou divergência não significa autorização para repetir.
8. Não pague QR, não transfira, não faça refund/cancelamento financeiro e não
   use AppID/host de produção. Pare os processos ao finalizar.

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
