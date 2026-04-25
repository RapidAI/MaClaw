import sqlite3
conn = sqlite3.connect('/data/soft/hubcenter/data/codeclaw-hubcenter.db')
cur = conn.cursor()
for row in cur.execute("select id,hub_id,email,is_default from hub_user_links where lower(email)=lower(?) order by hub_id,id", ('znsoft@163.com',)):
    print('|'.join('' if v is None else str(v) for v in row))
