import sqlite3, sys
for db in sys.argv[1:]:
    print('DB', db)
    conn = sqlite3.connect(db)
    cur = conn.cursor()
    try:
        row = cur.execute("select value_json from system_settings where key='center_registration'").fetchone()
        print(row[0] if row else '')
    except Exception as e:
        print('ERR', e)
