# url-insight

Projeto para análise e insights de URLs.

## Descrição

O url-insight é uma ferramenta desenvolvida em Go para processar, analisar e extrair informações relevantes de URLs. O objetivo é facilitar a obtenção de dados e estatísticas a partir de listas de endereços web.

## Estrutura do Projeto

- `/cmd/` - Aplicação principal (binário)
- `/pkg/` - Bibliotecas reutilizáveis
- `/internal/` - Código interno do projeto
- `/configs/` - Arquivos de configuração
- `/scripts/` - Scripts auxiliares
- `/docs/` - Documentação

## Instalação

```sh
git clone https://github.com/gildemberg-santos/url-insight.git
cd url-insight
go mod tidy
```

## Uso

```sh
go run ./cmd/main.go
```

## Configuração

| Variável | Descrição |
| --- | --- |
| `URLINSIGHT_DB` | Caminho do arquivo SQLite. Sem ela, usa `:memory:` (dados perdidos ao reiniciar). |
| `URLINSIGHT_ADMIN_TOKEN` | Se definida, `POST /shorten`, `GET /analytics` e `GET /show-data` exigem `Authorization: Bearer <token>` (a página `/metrics` pede o token ao receber 401). Sem ela, tudo fica aberto. |

| `URLINSIGHT_GA_ID` | ID do Google Analytics (ex.: `G-XXXXXXX`). Vazio: o script não é carregado. |
| `URLINSIGHT_FB_PIXEL_ID` | ID do Meta Pixel. Vazio: o script não é carregado. |
| `URLINSIGHT_CONTACT_EMAIL` | E-mail exibido na política de privacidade para pedidos de titulares. |

Um exemplo está em `.env.example`.

### Consentimento (LGPD)

GA e Meta Pixel só são carregados depois que o visitante aceita no banner da página inicial (a escolha fica no `localStorage`). Sem consentimento, `/r/<id>` não envia os cookies `_ga`/`_fbp` ao `/collect`. Quem chega direto a um link `/r/` sem ter decidido antes na página inicial não recebe trackers. A política fica em `/privacidade` e permite revogar a escolha. O texto é um modelo e deve ser revisado pelo responsável pelo tratamento dos dados. Bancos criados antes da coluna `created_at` são migrados automaticamente.

## Endpoints

- `POST /shorten` — cria um link: `{"url": "https://...", "short_id": "opcional"}` (retorna 201; 409 se o ID já existe).
- `GET /r/<short_id>` — página intermediária que coleta dados e redireciona.
- `POST /collect` — registra o payload (404 se o `short_id` não existe).
- `GET /analytics?short_id=` e `GET /show-data?short_id=` — JSON com estatísticas (incluindo `clicks_by_day`) / registros (com `created_at` em UTC).
- `GET /metrics?short_id=` — página de métricas.
- `GET /privacidade` — política de privacidade e revogação do consentimento.

## Contribuição

Contribuições são bem-vindas! Sinta-se à vontade para abrir issues e pull requests.

## Licença

Este projeto está licenciado sob a licença MIT.

---
