#!/usr/bin/env python3
"""Seed a local MeetHub with demo chat data through the public API.

Creates 30 demo users, 4 teams, public and private channels with history (one long
enough to page), threads, reactions, mentions, pinned messages, favorites, profile
pictures, direct and group conversations.
Idempotent: running it again changes nothing (messages use fixed pending_post_id
keys, existing users/teams/channels are reused).

Usage (the stack from `make up`, already set up with its owner):
  MEETHUB_DEMO_OWNER_EMAIL=ana@example.com \\
  MEETHUB_DEMO_PASSWORD=... \\
  python3 scripts/demo/seed_chat.py [http://localhost:8080]

Every created user gets MEETHUB_DEMO_PASSWORD. Demo data only: never run it against a real install.
"""
import http.cookiejar
import json
import os
import random
import struct
import sys
import time
import uuid
import zlib
import urllib.error
import urllib.request

BASE = (sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8080").rstrip("/")
PASSWORD = os.environ.get("MEETHUB_DEMO_PASSWORD") or sys.exit("set MEETHUB_DEMO_PASSWORD")
OWNER = os.environ.get("MEETHUB_DEMO_OWNER_EMAIL", "ana@example.com")

# email, display name, organization role
USERS = [
    ("bruno@example.com", "Bruno Martins", "admin"),
    ("carla@example.com", "Carla Pereira", "member"),
    ("diego@example.com", "Diego Rocha", "guest"),
    ("eduarda@example.com", "Eduarda Lima", "member"),
    ("felipe@example.com", "Felipe Souza", "member"),
    ("gabriela@example.com", "Gabriela Costa", "member"),
    ("henrique@example.com", "Henrique Alves", "member"),
    ("isabela@example.com", "Isabela Nunes", "member"),
    ("joao@example.com", "João Pereira", "member"),
    ("karina@example.com", "Karina Duarte", "admin"),
    ("lucas@example.com", "Lucas Azevedo", "member"),
    ("mariana@example.com", "Mariana Ribeiro", "member"),
    ("natalia@example.com", "Natália Campos", "member"),
    ("otavio@example.com", "Otávio Mendes", "member"),
    ("patricia@example.com", "Patrícia Gomes", "member"),
    ("rafael@example.com", "Rafael Teixeira", "member"),
    ("sabrina@example.com", "Sabrina Moraes", "member"),
    ("thiago@example.com", "Thiago Barros", "member"),
    ("ursula@example.com", "Úrsula Freitas", "member"),
    ("vinicius@example.com", "Vinícius Cardoso", "member"),
    ("wagner@example.com", "Wagner Pinto", "member"),
    ("ximena@example.com", "Ximena Vargas", "guest"),
    ("yasmin@example.com", "Yasmin Correia", "member"),
    ("zeca@example.com", "Zeca Monteiro", "member"),
    ("amanda@example.com", "Amanda Farias", "member"),
    ("caio@example.com", "Caio Nogueira", "member"),
    ("debora@example.com", "Débora Siqueira", "member"),
    ("emerson@example.com", "Emerson Leal", "member"),
    ("fabiana@example.com", "Fabiana Prado", "member"),
]

# Generated channels: team, name, purpose, type, number of members, number of messages, opening lines.
CHANNELS = [
    ("tecnologia", "backend", "APIs, banco de dados e filas.", "O", 14, 160, [
        "A migração `00002` rodou em homologação sem erros nos três bancos.",
        "Proposta: paginar o histórico por cursor em vez de offset.\n\n```go\nposts, next, err := svc.History(ctx, sub, channelID, cursor)\n```",
        "O p95 do `POST /posts` caiu de 42 ms para **18 ms** depois do índice composto.",
        "Alguém já olhou o *deadlock* intermitente no teste de concorrência do MariaDB?",
    ]),
    ("tecnologia", "frontend", "SPA, temas e acessibilidade.", "O", 12, 45, [
        "Exemplos de formatação para testar o chat:\n\n**negrito**, *itálico*, `código`, ~~riscado~~ e um [link](https://example.com/docs).\n\n> Citação: toda cor precisa passar no contraste AA.\n\n1. Primeiro\n2. Segundo\n3. Terceiro",
        "Tabela de temas:\n\n| Tema | Esquema | Contraste |\n| --- | --- | --- |\n| MeetHub Dark | escuro | AA |\n| Aurora | claro | AA |",
        "```ts\nconst unread = computed(() => channel.total_msg_count - channel.msg_count)\n```",
        "Os 22 temas passaram no teste de contraste :white_check_mark:",
    ]),
    ("tecnologia", "incidentes", "Incidentes em produção e post-mortems.", "O", 16, 30, [
        "**INC-204** aberto: latência alta no login. Investigando.",
        "Causa: pool de conexões do banco saturado por um job de relatório.",
        "Mitigado às 10h42. Post-mortem até sexta.",
    ]),
    ("tecnologia", "seguranca", "Vulnerabilidades e auditorias (privado).", "P", 5, 14, [
        "O `govulncheck` apontou uma CVE em dependência transitiva; atualização já em PR.",
        "Rotação das chaves de API concluída.",
    ]),
    ("produto", "pesquisa", "Entrevistas e achados com usuários.", "O", 8, 24, [
        "Resumo das 6 entrevistas: busca de mensagens é o pedido nº 1.",
        "4 de 6 participantes usam o chat principalmente pelo celular.",
    ]),
    ("produto", "lancamentos", "Notas de versão e comunicação.", "O", 10, 20, [
        "Rascunho das notas da **v2.4.0** pronto para revisão.",
        "Data do anúncio: quinta, 10h.",
    ]),
    ("comercial", "vendas", "Pipeline, propostas e fechamentos.", "O", 9, 40, [
        "Fechamos o contrato com a Acme :tada: 120 licenças.",
        "Pipeline da semana: 14 oportunidades, 5 em negociação.",
        "Preciso de um estudo de caso do setor financeiro para a proposta de amanhã.",
    ]),
    ("comercial", "marketing", "Campanhas, conteúdo e eventos.", "O", 8, 28, [
        "A campanha de e-mail teve 31% de abertura.",
        "Calendário de conteúdo de dezembro publicado.",
    ]),
    ("comercial", "parcerias", "Integradores e revendas.", "O", 6, 16, [
        "Reunião com o integrador do Sul remarcada para terça.",
    ]),
    ("comercial", "clientes-chave", "Contas estratégicas (privado).", "P", 4, 12, [
        "Renovação da conta Globex prevista para janeiro; risco baixo.",
    ]),
    ("pessoas", "avisos", "Comunicados para toda a empresa.", "O", 30, 18, [
        "@channel Recesso de fim de ano: de 23/12 a 02/01.",
        "A pesquisa de clima fecha na sexta. Participem!",
    ]),
    ("pessoas", "vagas", "Vagas abertas e indicações.", "O", 12, 15, [
        "Vaga aberta: Pessoa Desenvolvedora Go (pleno). Indicações são bem-vindas!",
    ]),
    ("pessoas", "beneficios", "Plano de saúde, vale e convênios.", "O", 14, 15, [
        "O novo convênio de academia começa em 1º de dezembro.",
    ]),
    ("pessoas", "onboarding", "Boas-vindas e primeiros passos.", "O", 10, 15, [
        "Boas-vindas a quem chegou esta semana! :wave: O guia de primeiros passos está fixado aqui.",
    ]),
]

STARTS = ["Atualizei", "Revisei", "Terminei", "Comecei", "Publiquei", "Validei", "Documentei", "Testei", "Ajustei", "Priorizei"]
THINGS = ["o relatório semanal", "a planilha de acompanhamento", "o plano da sprint", "a pauta da reunião",
          "o checklist de entrega", "a documentação", "o quadro de tarefas", "o resumo para a diretoria",
          "o cronograma", "a lista de pendências", "os indicadores do mês", "o material de apresentação"]
TAILS = ["hoje cedo.", "agora há pouco.", "e deixei o link no canal.", "— quem puder, dá uma olhada.",
         "e já está disponível para revisão.", "conforme combinamos ontem.", "antes da reunião das 15h.",
         "e marquei os pontos em aberto."]
SHORT = ["Bom dia, pessoal! :wave:", "Fechado, obrigado!", "Podemos falar disso na daily?", "Boa! :tada:",
         "Vou verificar e retorno em seguida.", "Perfeito, sigo com isso.", "Qual o prazo para isso?", "Combinado.",
         "Alguém consegue revisar ainda hoje?", "Boa pergunta, vou confirmar com o time.", "Feito :white_check_mark:",
         "Subi a alteração, podem testar."]
REPLIES = ["Concordo.", "Posso ajudar com isso.", "Já está no meu radar.", "Obrigado pelo aviso!", "Vou olhar agora.",
           "Tem um detalhe: precisamos avisar o suporte antes.", "Ficou ótimo :clap:", "Deixei um comentário no documento."]
EMOJI = ["+1", "tada", "rocket", "eyes", "heart", "white_check_mark", "fire", "pray", "joy", "ok_hand", "thinking",
         "raised_hands"]
TALK = ["Oi! Tem cinco minutos para alinharmos uma coisa?", "Claro, pode falar.", "Te mandei o documento por e-mail.",
        "Recebi, obrigado! Leio ainda hoje.", "Consegue cobrir a reunião das 14h para mim?", "Consigo sim, sem problema.",
        "Ficou alguma pendência da semana passada?", "Só a revisão do relatório; termino amanhã.", "Almoço hoje?",
        "Bora! 12h30 na portaria.", "Valeu pela ajuda ontem :pray:", "Imagina, precisando é só chamar."]


class Client:
    def __init__(self, email):
        self.jar = http.cookiejar.CookieJar()
        self.op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))
        self.csrf = None
        status, body = self.call("POST", "/auth/login", {"email": email, "password": PASSWORD})
        if status != 200:
            sys.exit(f"login {email}: {status} {body}")
        self.csrf = body["csrf_token"]
        self.me = body["user"]

    def call(self, method, path, body=None):
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(BASE + "/api/v1" + path, method=method, data=data)
        req.add_header("Content-Type", "application/json")
        req.add_header("Origin", BASE)
        if self.csrf:
            req.add_header("X-CSRF-Token", self.csrf)
        try:
            with self.op.open(req) as r:
                raw = r.read()
                return r.status, (json.loads(raw) if raw else None)
        except urllib.error.HTTPError as e:
            raw = e.read()
            return e.code, (json.loads(raw) if raw else None)

    def upload(self, path, filename, data):
        """POST a multipart form with the file in the field "image"."""
        boundary = uuid.uuid4().hex
        body = (f'--{boundary}\r\nContent-Disposition: form-data; name="image"; filename="{filename}"\r\n'
                f"Content-Type: image/png\r\n\r\n").encode() + data + f"\r\n--{boundary}--\r\n".encode()
        req = urllib.request.Request(BASE + "/api/v1" + path, method="POST", data=body)
        req.add_header("Content-Type", f"multipart/form-data; boundary={boundary}")
        req.add_header("Origin", BASE)
        req.add_header("X-CSRF-Token", self.csrf)
        try:
            with self.op.open(req) as r:
                return r.status
        except urllib.error.HTTPError as e:
            sys.exit(f"{self.me['email']} upload {path}: {e.code} {e.read()[:200]}")

    def ok(self, method, path, body=None, allow=()):
        while True:
            status, out = self.call(method, path, body)
            if status != 429:
                break
            time.sleep(2)  # the per-user message limit (30 in 10 s): wait and repeat
        code = (out or {}).get("error", {}).get("code") if isinstance(out, dict) else None
        if status >= 400 and code not in allow:
            sys.exit(f"{self.me['email']} {method} {path}: {status} {out}")
        return out if status < 400 else None


def avatar_png(color, size=128):
    """A simple portrait silhouette (head and shoulders) on a colored square, as PNG bytes."""
    r, g, b = (int(color[i:i + 2], 16) for i in (1, 3, 5))
    light = (min(255, r + 110), min(255, g + 110), min(255, b + 110))
    rows = []
    for y in range(size):
        row = bytearray([0])
        for x in range(size):
            head = (x - size / 2) ** 2 + (y - size * 0.40) ** 2 <= (size * 0.19) ** 2
            body = ((x - size / 2) / (size * 0.36)) ** 2 + ((y - size * 1.02) / (size * 0.36)) ** 2 <= 1
            row += bytes(light if head or body else (r, g, b))
        rows.append(bytes(row))

    def chunk(kind, data):
        body = kind + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body))

    ihdr = struct.pack(">IIBBBBB", size, size, 8, 2, 0, 0, 0)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) + chunk(b"IDAT", zlib.compress(b"".join(rows))) + chunk(b"IEND", b"")


AVATAR_COLORS = ["#3d41cf", "#0f766e", "#9b1c1c", "#7c3aed", "#b45309", "#0369a1", "#be185d", "#166534"]


def ensure_users(owner):
    existing = {}
    cursor = ""
    while True:
        page = owner.ok("GET", "/admin/users?limit=200" + (f"&cursor={cursor}" if cursor else ""))
        for u in page["users"]:
            existing[u["email"]] = u["id"]
        cursor = page.get("next_cursor")
        if not cursor:
            break
    for email, name, role in USERS:
        if email not in existing:
            u = owner.ok("POST", "/admin/users", {"email": email, "display_name": name, "role": role, "password": PASSWORD})
            existing[email] = u["id"]
            print("user", email, role)
    return existing


def ensure_team(owner, name, display, description):
    mine = owner.ok("GET", "/users/me/teams")["items"]
    for t in mine:
        if t["name"] == name:
            return t["id"]
    t = owner.ok("POST", "/teams", {"name": name, "display_name": display, "description": description})
    print("team", name)
    return t["id"]


def ensure_channel(owner, team, name, display, purpose, typ="O"):
    for path in (f"/users/me/teams/{team}/channels?per_page=200", f"/teams/{team}/channels?per_page=200"):
        for c in owner.ok("GET", path)["items"]:
            if c["name"] == name:
                owner.ok("POST", f"/channels/{c['id']}/members", {}, allow=("forbidden",))
                return c["id"]
    c = owner.ok("POST", "/channels", {"team_id": team, "name": name, "display_name": display, "purpose": purpose, "type": typ})
    print("channel", name)
    return c["id"]


def main():
    owner = Client(OWNER)
    ids = ensure_users(owner)
    c = {email: Client(email) for email, _, _ in USERS}
    c[OWNER] = owner
    uid = {email: cl.me["id"] for email, cl in c.items()}
    uname = {email: cl.me["username"] for email, cl in c.items()}

    tec = ensure_team(owner, "tecnologia", "Tecnologia", "Engenharia, infraestrutura e suporte")
    prod = ensure_team(owner, "produto", "Produto", "Produto, design e pesquisa")
    tec_people = ["bruno@example.com", "carla@example.com", "eduarda@example.com", "felipe@example.com",
                  "henrique@example.com", "joao@example.com", "diego@example.com"]
    prod_people = ["carla@example.com", "gabriela@example.com", "isabela@example.com", "joao@example.com"]
    for email in tec_people:
        owner.ok("POST", f"/teams/{tec}/members", {"user_id": uid[email]})
    for email in prod_people:
        owner.ok("POST", f"/teams/{prod}/members", {"user_id": uid[email]})

    projetos = ensure_channel(owner, tec, "projetos", "projetos", "Planejamento, execução e acompanhamento dos projetos do time.")
    devops = ensure_channel(owner, tec, "devops", "devops", "Pipelines, deploys e observabilidade.")
    infra = ensure_channel(owner, tec, "infra", "infra", "Servidores, rede e custos de nuvem.")
    suporte = ensure_channel(owner, tec, "suporte", "suporte", "Chamados e dúvidas dos usuários.")
    financeiro = ensure_channel(owner, tec, "financeiro", "financeiro", "Orçamento de infraestrutura (privado).", "P")
    roadmap = ensure_channel(owner, prod, "roadmap", "roadmap", "Prioridades do trimestre.")
    design = ensure_channel(owner, prod, "design", "design", "Protótipos e revisões de interface.")

    def join(channel, people, admin=owner):
        for email in people:
            cl = c[email]
            if cl.ok("POST", f"/channels/{channel}/members", {}, allow=("forbidden", "not_found")) is None:
                admin.ok("POST", f"/channels/{channel}/members", {"user_id": uid[email]}, allow=("not_team_member",))

    join(projetos, ["bruno@example.com", "carla@example.com", "diego@example.com", "eduarda@example.com", "felipe@example.com"])
    join(devops, ["bruno@example.com", "felipe@example.com", "henrique@example.com"])
    join(infra, ["bruno@example.com", "henrique@example.com", "joao@example.com"])
    join(suporte, ["carla@example.com", "eduarda@example.com", "joao@example.com", "diego@example.com"])
    join(financeiro, ["bruno@example.com", "henrique@example.com"])
    join(roadmap, ["carla@example.com", "gabriela@example.com", "isabela@example.com"])
    join(design, ["gabriela@example.com", "isabela@example.com"])

    def post(key, email, channel, message, root=None):
        body = {"channel_id": channel, "message": message, "pending_post_id": "seed-" + key}
        if root:
            body["root_id"] = root
        return c[email].ok("POST", "/posts", body)["id"]

    def react(email, post_id, *emojis):
        for e in emojis:
            c[email].ok("POST", "/reactions", {"post_id": post_id, "emoji_name": e})

    A, B, C, D = OWNER, "bruno@example.com", "carla@example.com", "diego@example.com"
    E, F, H, J = "eduarda@example.com", "felipe@example.com", "henrique@example.com", "joao@example.com"
    G, I = "gabriela@example.com", "isabela@example.com"

    # #projetos — the mockup's conversation
    p1 = post("p1", A, projetos, "Pessoal, bom dia! :wave:\nVamos alinhar o deploy da **v2.4.0** ainda hoje. A infra já está "
              "preparada e os testes no ambiente de homologação passaram ontem. Podemos seguir com o deploy às 17h?")
    react(B, p1, "+1"); react(C, p1, "+1", "white_check_mark"); react(E, p1, "+1"); react(F, p1, "white_check_mark")
    p2 = post("p2", B, projetos, "Do lado de infra, está tudo certo. A pipeline no CI/CD já está atualizada e o ambiente de "
              "produção está pronto.\nSó peço que a gente monitore os logs por ~30 min após o deploy, como de costume.")
    react(A, p2, "ok_hand"); react(C, p2, "ok_hand"); react(F, p2, "hammer_and_wrench")
    p3 = post("p3", C, projetos, "Perfeito! Vou finalizar a revisão das últimas PRs até o meio-dia. Também atualizo a "
              "documentação da release com as mudanças e o rollback, se necessário.")
    post("p3r1", A, projetos, "Obrigada, @" + uname[C] + "! Me marca quando terminar.", root=p3)
    post("p3r2", C, projetos, "Feito: changelog e plano de rollback em `docs/release-2.4.0.md`.", root=p3)
    p4 = post("p4", D, projetos, "Sugiro fazermos uma sync rápida antes do deploy para confirmar os últimos pontos (5–10 min).\n"
              "Alguém consegue criar a reunião?")
    react(A, p4, "raised_hand")
    p5 = post("p5", A, projetos, "Já criei a reunião da sync! Vamos alinhar às 15h. @channel")
    react(B, p5, "rocket"); react(C, p5, "rocket"); react(E, p5, "calendar"); react(F, p5, "rocket")
    p6 = post("p6", E, projetos, "Checklist do deploy:\n1. Congelar merges às 16h30\n2. Backup do banco\n3. Deploy canário (10%)\n"
         "4. Monitorar erros e latência\n5. Liberar 100%")
    post("p7", F, projetos, "Os testes de carga ficaram dentro do esperado: p95 de **180 ms** com 2× o tráfego atual.")

    # #devops, #infra, #suporte, #financeiro
    d1 = post("d1", F, devops, "O job de build está levando 12 min. Proposta: cache das dependências do Go e do npm entre execuções.")
    post("d1r1", B, devops, "Boa! Pode abrir a PR? Eu reviso hoje.", root=d1)
    post("d1r2", F, devops, "PR aberta: tempo caiu para 4 min :tada:", root=d1)
    react(H, d1, "+1")
    post("d2", H, devops, "Alerta de disco em `worker-03` resolvido: rotação de logs ajustada para 7 dias.")
    post("i1", H, infra, "Custos de nuvem de outubro: −8% depois do desligamento dos ambientes de teste fora do horário.")
    post("i2", J, infra, "Vou migrar o Redis para a versão 8.2 na janela de quinta, 22h. Alguma objeção?")
    post("i3", B, infra, "Sem objeção. Lembra de validar o AOF depois da migração.")
    s1 = post("s1", D, suporte, "Um cliente relatou lentidão no login às 9h. Alguém viu algo nos dashboards?")
    post("s1r1", E, suporte, "Foi um pico de tráfego da campanha de e-mail; o autoscaling segurou em 3 min.", root=s1)
    post("s2", J, suporte, "Atualizei a página de ajuda sobre redefinição de senha.")
    post("f1", B, financeiro, "Orçamento de infra do Q1 aprovado: manter reserva de 15% para picos.")
    post("f2", H, financeiro, "Vou cotar instâncias reservadas por 1 ano; economia estimada de 22%.")

    # Produto
    post("r1", G, roadmap, "Prioridades do trimestre: busca de mensagens, arquivos no chat e salas de reunião.")
    post("r2", C, roadmap, "Concordo. A busca depende da indexação; sugiro começar por ela.")
    post("r3", I, roadmap, "Pesquisa com usuários marcada para terça: 6 entrevistas.")
    post("g1", I, design, "Novo protótipo da barra lateral com indicadores de presença. Feedback até sexta, por favor!")
    post("g2", G, design, "Ficou ótimo. Só aumentaria o contraste do badge de menções nos temas escuros.")

    # direct and group conversations
    dm_ab = owner.ok("POST", "/channels/direct", {"user_id": uid[B]})["id"]
    post("dm1", A, dm_ab, "Bruno, você consegue acompanhar o deploy comigo às 17h?")
    post("dm2", B, dm_ab, "Consigo sim. Já deixo os dashboards abertos.")
    dm_ac = owner.ok("POST", "/channels/direct", {"user_id": uid[C]})["id"]
    post("dm3", C, dm_ac, "Ana, terminei a revisão das PRs :white_check_mark:")
    post("dm4", A, dm_ac, "Show! Obrigada, Carla.")
    dm_ce = c[C].ok("POST", "/channels/direct", {"user_id": uid[E]})["id"]
    post("dm5", E, dm_ce, "Carla, pode revisar meu texto da página de ajuda?")
    gm = owner.ok("POST", "/channels/group", {"user_ids": [uid[B], uid[C]]})["id"]
    post("gm1", A, gm, "Grupo para fechar os detalhes da release 2.4.0.")
    post("gm2", B, gm, "Fechado. Proponho o anúncio às 18h, depois de 1 h de monitoramento.")
    post("gm3", C, gm, "Concordo. Rascunho do anúncio está pronto.")
    gm2 = c[F].ok("POST", "/channels/group", {"user_ids": [uid[H], uid[J], uid[B]]})["id"]
    post("gm4", F, gm2, "Pessoal de plantão desta semana: confirmem a escala, por favor.")
    post("gm5", H, gm2, "Confirmado para terça e quarta.")
    post("gm6", J, gm2, "Fico com quinta e sexta.")

    # ── a larger workspace: two more teams, channels with history, threads, mentions, more conversations ──
    rnd = random.Random(2026)  # fixed: every run generates the same content under the same keys
    com = ensure_team(owner, "comercial", "Comercial", "Vendas, marketing e parcerias")
    pes = ensure_team(owner, "pessoas", "Pessoas", "RH, vagas e benefícios")
    everyone = [email for email, _, _ in USERS]
    teams = {"tecnologia": (tec, everyone[:18]), "produto": (prod, everyone[8:22]),
             "comercial": (com, everyone[16:29]), "pessoas": (pes, everyone)}
    for team, people in teams.values():
        for email in people:
            owner.ok("POST", f"/teams/{team}/members", {"user_id": uid[email]})

    posts = 0
    for team_name, name, purpose, typ, size, count, opening in CHANNELS:
        team, people = teams[team_name]
        ch = ensure_channel(owner, team, name, name, purpose, typ)
        members = rnd.sample(people, min(size, len(people)))
        join(ch, members)
        members.append(OWNER)
        for i in range(count):
            author = members[i % len(members)]
            if i < len(opening):
                text = opening[i]
            elif i % 4 == 3:
                text = rnd.choice(SHORT)
            else:
                text = f"{rnd.choice(STARTS)} {rnd.choice(THINGS)} {rnd.choice(TAILS)}"
            if i % 9 == 8:  # a mention: of Ana every other time, so the owner has badges to clear
                target = OWNER if i % 18 == 17 else rnd.choice(members)
                if target != author:
                    text = f"@{uname[target]} {text[0].lower()}{text[1:]}"
            key = f"g-{team_name}-{name}-{i}"
            root = post(key, author, ch, text)
            posts += 1
            if i % 6 == 5:
                for j in range(rnd.randint(1, 4)):
                    post(f"{key}-r{j}", rnd.choice(members), ch, rnd.choice(REPLIES), root=root)
                    posts += 1
            if i % 3 == 0:
                for email in rnd.sample(members, min(len(members), rnd.randint(1, 4))):
                    react(email, root, rnd.choice(EMOJI))

    # direct conversations: Ana with ten people, and ten other pairs
    pairs = [(OWNER, e) for e in everyone[9:19]] + [(everyone[i], everyone[i + 7]) for i in range(10, 20)]
    for n, (a, b) in enumerate(pairs):
        dm = c[a].ok("POST", "/channels/direct", {"user_id": uid[b]}, allow=("forbidden",))
        if dm is None:
            continue  # a guest without a shared channel
        start = rnd.randrange(0, len(TALK), 2)
        for k in range(rnd.randint(2, 6)):
            post(f"gdm-{n}-{k}", (b, a)[k % 2], dm["id"], TALK[(start + k) % len(TALK)])
            posts += 1
    groups = [[OWNER, everyone[10], everyone[11], everyone[12]], [OWNER, everyone[20], everyone[21]],
              [everyone[13], everyone[14], everyone[15], everyone[16], everyone[17]],
              [everyone[22], everyone[23], everyone[24]], [everyone[1], everyone[25], everyone[26], everyone[27]]]
    for n, people in enumerate(groups):
        g = c[people[0]].ok("POST", "/channels/group", {"user_ids": [uid[e] for e in people[1:]]})
        for k in range(rnd.randint(3, 7)):
            post(f"ggm-{n}-{k}", people[k % len(people)], g["id"], rnd.choice(SHORT + TALK))
            posts += 1

    # pins, favorites and profile pictures (the layout's banner, the Favorites section, pictures beside names)
    owner.ok("POST", f"/posts/{p6}/pin", {})
    c[C].ok("POST", f"/posts/{p3}/pin", {})
    owner.ok("PUT", f"/users/me/channels/{projetos}/favorite", {"favorite": True})
    owner.ok("PUT", f"/users/me/channels/{dm_ab}/favorite", {"favorite": True})
    c[B].ok("PUT", f"/users/me/channels/{devops}/favorite", {"favorite": True})
    pictures = 0
    for n, email in enumerate([OWNER] + everyone[:15]):
        me = c[email].ok("GET", "/me")["user"]
        if not me.get("last_picture_update"):  # only once: a rerun changes nothing
            c[email].upload("/users/me/image", "avatar.png", avatar_png(AVATAR_COLORS[n % len(AVATAR_COLORS)]))
            pictures += 1
    if pictures:
        print("pictures", pictures)

    print(f"done: {len(USERS) + 1} users, 4 teams, {7 + len(CHANNELS)} channels, {len(pairs) + 3} direct and "
          f"{len(groups) + 2} group conversations, {posts} generated messages")


if __name__ == "__main__":
    main()
