# Plano de rollback — v2.4.0

**Janela de deploy:** hoje, 17h–18h · **Responsável:** Bruno Martins · **Aprovação:** Ana Silva

## Quando reverter

- Taxa de erro acima de 2% por 5 minutos após o deploy.
- Latência p95 acima de 800 ms por 10 minutos.
- Falha em qualquer verificação do checklist pós-deploy.

## Passos

1. Pausar a pipeline de deploy no CI/CD.
2. Reverter a imagem para `v2.3.7` no ambiente de produção.
3. Executar `migrate down` apenas se a migração 0042 tiver sido aplicada.
4. Validar os fluxos de login, pagamento e relatórios.
5. Avisar em #projetos e abrir o post-mortem.

## Contatos

- Infra: @bruno · Aplicação: @carla · Produto: @ana
