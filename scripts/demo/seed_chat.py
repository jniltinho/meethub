#!/usr/bin/env python3
"""Seed a local MeetHub with demo chat data through the public API.

Creates demo users, teams, public and private channels, threads, reactions,
direct and group conversations. Idempotent: running it again changes nothing
(messages use fixed pending_post_id keys, existing users/teams/channels are reused).

Usage (the stack from `make up`, already set up with its owner):
  MEETHUB_DEMO_OWNER_EMAIL=ana@example.com \\
  MEETHUB_DEMO_PASSWORD=... \\
  python3 scripts/demo/seed_chat.py [http://localhost:8080]

Every created user gets MEETHUB_DEMO_PASSWORD. Demo data only: never run it against a real install.
"""
import http.cookiejar
import json
import os
import sys
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
]


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

    def ok(self, method, path, body=None, allow=()):
        status, out = self.call(method, path, body)
        code = (out or {}).get("error", {}).get("code") if isinstance(out, dict) else None
        if status >= 400 and code not in allow:
            sys.exit(f"{self.me['email']} {method} {path}: {status} {out}")
        return out if status < 400 else None


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
    post("p6", E, projetos, "Checklist do deploy:\n1. Congelar merges às 16h30\n2. Backup do banco\n3. Deploy canário (10%)\n"
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

    print("done:", len(USERS) + 1, "users, 2 new teams, 7 channels, 3 direct and 2 group conversations")


if __name__ == "__main__":
    main()
