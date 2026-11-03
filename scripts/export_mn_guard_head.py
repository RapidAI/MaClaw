# Export round-1 control heads C_qo (5 final seeds, TC calibration) as one exact
# LocalHead JSON: hidden layers concatenated, w2 averaged over seeds, per-class T folded in.
# Guard: tau=.971 (pre-registered, Go-Q8 re-check PASS), sensitive labels never accepted (tau_s=2).
import json,pickle,sys,numpy as np
Q='/workspace/qlora/work/intent/'; D=pickle.load(open(Q+'C_qo.pkl','rb')); LAB=D['LAB']
C=json.load(open(Q+'sel_qo.json'))['best_nomb']['C_qo|TC']; T=np.array(C['T_vec'])
meta=json.load(open('/workspace/r15b/intent_meta.json')); SENS=sorted(set(meta['sensitive'])|{'mailbox_send'})
FW=[W for k,(_,_,W,_) in sorted(D['res'].items()) if k[2]=='final']; assert len(FW)==5
w1=np.concatenate([W[0].T for W in FW],0); b1=np.concatenate([W[1] for W in FW])
w2=np.concatenate([W[2].T for W in FW],1)/len(FW)/T[:,None]; b2=np.mean([W[3] for W in FW],0)/T
r=lambda a:[float('%.6g'%v) for v in a]
out=dict(labels=LAB,w1=[r(x) for x in w1],b1=r(b1),w2=[r(x) for x in w2],b2=r(b2),temperature=1.0,tau=0.971,tau_s=2.0,l2norm=True,
 sensitive=SENS,input_blocks=['last','mean'],embed_role='classification',embed_model='qwen3',
 calibration={'source':'mn_conf_guard_goq8','compute':'goq8'},
 note='MN (Cyrillic) confidence guard: C_qo 5-seed ensemble, TC calibration; accept iff p>=.971 and label not sensitive (STATUS_mn_conf_guard 2003fda, REPORT e5783de)')
json.dump(out,open(sys.argv[1],'w'),separators=(',',':'))
# parity fixture
rng=np.random.default_rng(7); X=rng.standard_normal((3,2048)).astype(np.float32)
nz=lambda a:a/np.linalg.norm(a,axis=1,keepdims=True); x=np.concatenate([nz(X[:,:1024]),nz(X[:,1024:])],1)
Z=np.mean([np.maximum(x@W[0]+W[1],0)@W[2]+W[3] for W in FW],0)/T; P=np.exp(Z-Z.max(1,keepdims=True)); P/=P.sum(1,keepdims=True)
json.dump(dict(vecs=X.tolist(),probs=P.tolist()),open(sys.argv[2],'w'))
print('ok',w1.shape,w2.shape)
