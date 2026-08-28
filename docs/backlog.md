# Backlog

> Itens observados durante o desenvolvimento que precisam ser avaliados para possível
> virgem a histórias em fases futuras. Não são bugs nem features planejadas — são
> melhorias, ideias e ajustes que surgiram no caminho.

---

## Performance

- **Seek no vídeo é lento:** navegar para um momento específico na linha do tempo do
  vídeo apresenta latência alta. Investigar causa (asset handler, encoding, buffering)
  e considerar otimizações como preload de trechos, transcodificação ou ajustes no
  `<video>` element. **Atualização 28/08/2026:** o vídeo no Linux nem tocava até essa
  data (ver `docs/fase-1-mvp.md`) — o asset handler trocou de um `application.Middleware`
  do Wails (scheme `wails://`) pra um `http.Server` real em loopback
  (`services/video_server.go`). Reavaliar se esse item ainda se aplica com o servidor
  novo antes de investigar mais.

## UX — Transcrição

- **Falas em colunas lado a lado:** opção de exibir as falas do tutor e do estudante
  em duas colunas sincronizadas (tutor à esquerda, estudante à direita), mantendo a
  rolagem e o highlight do trecho atual alinhados entre as colunas. Alternativa à
  visão linear atual para facilitar a leitura do fluxo da conversa.

- **Edição de transcrição:** possibilidade de corrigir erros no texto da transcrição
  diretamente na UI (ex.: palavras em outra língua que o STT não reconheceu
  corretamente, gírias, trechos truncados). Considerar se a correção atualiza apenas
  o texto exibido ou também a entrada no banco.
