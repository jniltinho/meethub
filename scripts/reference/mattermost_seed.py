import json, urllib.request
B="http://127.0.0.1:8065/api/v4"
def req(method, path, body=None, token=None):
    r=urllib.request.Request(B+path, method=method, data=json.dumps(body).encode() if body is not None else None,
        headers={"Content-Type":"application/json", **({"Authorization":"Bearer "+token} if token else {})})
    with urllib.request.urlopen(r) as resp:
        return resp.headers.get("Token"), json.loads(resp.read() or b"null")
tok={}; uid={}
for u in ["ana.silva","bruno.martins","carla.pereira","diego.rocha"]:
    t,me=req("POST","/users/login",{"login_id":u,"password":"Meethub-demo-2026!"}); tok[u]=t; uid[u]=me["id"]
_,team=req("GET","/teams/name/tecnologia",token=tok["ana.silva"])
_,ch=req("GET",f"/teams/{team['id']}/channels/name/projetos",token=tok["ana.silva"])
def post(u, msg, root=""):
    _,p=req("POST","/posts",{"channel_id":ch["id"],"message":msg,"root_id":root},tok[u]); return p
def react(u, p, e): req("POST","/reactions",{"user_id":uid[u],"post_id":p["id"],"emoji_name":e},tok[u])
p1=post("ana.silva","Pessoal, bom dia! :wave:\nVamos alinhar o deploy da v2.4.0 ainda hoje. A infra já está preparada e os testes no ambiente de homologação passaram ontem. Podemos seguir com o deploy às 17h?")
react("bruno.martins",p1,"+1"); react("carla.pereira",p1,"+1"); react("diego.rocha",p1,"white_check_mark")
p2=post("bruno.martins","Do lado de infra, está tudo certo. A pipeline no CI/CD já está atualizada e o ambiente de produção está pronto.\nSó peço que a gente monitore os logs por ~30 min após o deploy, como de costume.")
react("ana.silva",p2,"ok_hand")
p3=post("carla.pereira","Perfeito! Vou finalizar a revisão das últimas PRs até o meio-dia. Também atualizo a **documentação da release** com as mudanças e o rollback, se necessário.")
post("ana.silva","Ótimo, obrigada @carla.pereira!",p3["id"]); post("diego.rocha","Posso ajudar na revisão do rollback.",p3["id"])
post("diego.rocha","Sugiro fazermos uma sync rápida antes do deploy para confirmar os últimos pontos (5–10 min).\nAlguém consegue criar a reunião?")
post("ana.silva","Já criei a reunião da sync! Vamos alinhar às 15h. `make deploy VERSION=v2.4.0`\n\n```bash\nkubectl rollout status deploy/meethub\n```")
# a DM and a group message
_,dm=req("POST","/channels/direct",[uid["ana.silva"],uid["bruno.martins"]],tok["ana.silva"])
req("POST","/posts",{"channel_id":dm["id"],"message":"Bruno, consegue revisar o checklist do deploy?"},tok["ana.silva"])
_,gm=req("POST","/channels/group",[uid["ana.silva"],uid["carla.pereira"],uid["diego.rocha"]],tok["ana.silva"])
req("POST","/posts",{"channel_id":gm["id"],"message":"Grupo da release: vamos usar este canal para os detalhes."},tok["ana.silva"])
# an unread mention for ana in #devops
_,dv=req("GET",f"/teams/{team['id']}/channels/name/devops",token=tok["bruno.martins"])
req("POST","/posts",{"channel_id":dv["id"],"message":"@ana.silva pode olhar o alerta de disco do node-3?"},tok["bruno.martins"])
print("seeded", ch["id"])
