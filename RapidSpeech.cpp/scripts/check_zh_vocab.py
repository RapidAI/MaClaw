#!/usr/bin/env python3
import json, os
os.chdir(os.path.join(os.path.dirname(__file__), ".."))
with open("models/moonshine-base-zh/tokenizer.json","r",encoding="utf-8") as f:
    d=json.load(f)
v=d.get("model",{}).get("vocab",{})
id2t={v:k for k,v in v.items()}
for i in [0,1,2,3,4,5,10,50,100,500,1000,5000,10000,20000,30000,30210,30257]:
    t = id2t.get(i, "?")
    print(f"  {i}: {repr(t)}")
bt=[t for t in id2t.values() if t.startswith("<0x")]
print(f"Byte tokens: {len(bt)}")
print(f"Total vocab: {len(id2t)}")
# What did greedy decode produce? Check what token repeats
# The greedy output was empty string - likely token that decodes to empty
# Check tokens that map to empty or whitespace
empty_toks = [(i,t) for i,t in id2t.items() if t.strip() == "" or t == "\u2581"]
print(f"Empty/space tokens: {empty_toks[:10]}")
