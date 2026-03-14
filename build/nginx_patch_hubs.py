from pathlib import Path
import shutil
from datetime import datetime

conf = Path("/soft/nginx/conf/nginx.conf")
text = conf.read_text()

marker = "server_name hubs.rapidai.tech;"
if marker not in text:
    block = """

    server {
        listen 80;
        server_name hubs.rapidai.tech;

        location / {
            proxy_pass http://127.0.0.1:9399;
            proxy_http_version 1.1;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection "upgrade";
        }
    }

    server {
        listen 443 ssl;
        server_name hubs.rapidai.tech;

        ssl_certificate /soft/nginx/conf/crt/alldomain.cert;
        ssl_certificate_key /soft/nginx/conf/crt/alldomain.key;
        ssl_session_timeout 5m;
        ssl_protocols TLSv1 TLSv1.1 TLSv1.2;
        ssl_session_cache shared:SSL:1m;
        ssl_ciphers HIGH:!aNULL:!MD5;
        ssl_prefer_server_ciphers on;

        location / {
            proxy_pass http://127.0.0.1:9399;
            proxy_http_version 1.1;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection "upgrade";
        }
    }
"""
    stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    backup = conf.with_name(f"{conf.name}.bak_{stamp}")
    shutil.copy2(str(conf), str(backup))

    idx = text.rfind("}")
    if idx == -1:
        raise SystemExit("Could not find closing brace in nginx.conf")
    conf.write_text(text[:idx] + block + "\n}\n")
    print(str(backup))
else:
    print("ALREADY_PRESENT")
