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
| `URLINSIGHT_ADMIN_TOKEN` | Se definida, `POST /shorten` exige `Authorization: Bearer <token>`. |

## Endpoints

- `POST /shorten` — cria um link: `{"url": "https://...", "short_id": "opcional"}` (retorna 201; 409 se o ID já existe).
- `GET /r/<short_id>` — página intermediária que coleta dados e redireciona.
- `POST /collect` — registra o payload (404 se o `short_id` não existe).
- `GET /analytics?short_id=` e `GET /show-data?short_id=` — JSON com estatísticas / registros.
- `GET /metrics?short_id=` — página de métricas.

## Contribuição

Contribuições são bem-vindas! Sinta-se à vontade para abrir issues e pull requests.

## Licença

Este projeto está licenciado sob a licença MIT.

---
