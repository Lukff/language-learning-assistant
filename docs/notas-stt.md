# Notas de qualidade — comparação de STT

> Registro de observações por provedor/aula, seguindo os critérios de comparação definidos em
> `docs/fase-0-validacao.md` (História 2). Anotações **parafraseadas** — sem transcrever trechos
> literais da fala, nomes de tutores ou qualquer dado que identifique aula/pessoa específica.
> Alimenta a tabela comparativa final da História 2.

## Gladia

### Aula 01

- **Qualidade geral:** boa.
- **Diarização:** confunde os locutores em trechos de sobreposição de fala (quando um interlocutor
  começa a falar antes do outro terminar a frase).
- **Inglês com sotaque do aluno:**
- **Code-switching PT/ES:** detecção funciona bem no geral, mas a transcrição se perde em alguns
  momentos específicos da troca de idioma.
- **Timestamps por palavra:** bons.
- **Pontuação/formatação:** pontuação nem sempre condizente com a fala; algumas pausas são
  registradas como pontuação de um jeito que não corresponde ao que foi dito.
- **Custo real:** free tier de 10h/mês de transcrição.
- **Ergonomia da API:**
- **Outras observações:** perda de algumas palavras de transição entre frases (fora dos trechos
  de sobreposição de fala) e de bastante "filler words" (palavras de preenchimento, tipo
  hesitações) de forma geral.

## AssemblyAI

### Aula 01

- **Qualidade geral:**
- **Diarização:** separa muito bem os locutores, inclusive em trechos de sobreposição de fala.
- **Inglês com sotaque do aluno:**
- **Code-switching PT/ES:** consegue fazer a troca de idioma em alguns pontos, mas se confunde
  bastante quando são usadas palavras isoladas (fora de uma frase inteira no outro idioma).
- **Timestamps por palavra:** precisos.
- **Pontuação/formatação:** pontuação das frases fica bem precisa.
- **Custo real:** preço oficial é US$ 0,21/h pelo uso do modelo + US$ 0,02/h pela diarização
  (cobrados separadamente, mas ambos pela duração real do áudio) — em um vídeo de 29:16min, o
  custo real observado foi US$ 0,1025 (modelo) + US$ 0,0098 (diarização) = US$ 0,11225 total. Há
  também a opção de incluir termos previamente conhecidos para melhorar a detecção, com custo
  adicional de US$ 0,05/h. Tem US$ 50 de créditos free, únicos (não é um free tier mensal
  recorrente).
- **Ergonomia da API:**
- **Outras observações:**
