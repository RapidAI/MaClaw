from pathlib import Path
p = Path(r"D:\workprj\aicoder\hubcenter\web\admin\assets\js\llm-service-tab.js")
text = p.read_text(encoding="utf-8")

repls = [
(
"""      sgClassTraffic: 'Downstream traffic', sgClassTrafficHint: 'Successful requests billed to this group, by task class. Training lives on the Classification head tab.',
      sgClassTrafficOpen: 'Traffic',
      sgTryRules: 'Try rules', sgTryPlaceholder: 'write a business plan',
      sgTryWorkflow: 'Workflow type', sgTryPhase: 'Phase kind', sgTryTask: 'Task type',
      sgClassTrain: 'Classification traffic',
      sgClassTrainHint: 'This group only shows billed class traffic. Train the one HubCenter head on the Classification head tab.',
      sgClassTrainOpen: 'Traffic', sgClassTrainNeedSave: 'Save this dynamic group first to see its classification traffic.',
""",
"""      sgClassTraffic: 'Downstream traffic', sgClassTrafficHint: 'Successful requests billed to this group, by task class. Training lives on the Classification head tab.',
      sgClassTrafficOpen: 'Traffic',
      sgTryRules: 'Try rules', sgTryPlaceholder: 'write a business plan',
      sgTryWorkflow: 'Workflow type', sgTryPhase: 'Phase kind', sgTryTask: 'Task type',
"""
),
(
"""      sgClassTraffic: '\\u4e0b\\u6e38\\u6d41\\u91cf', sgClassTrafficHint: '\\u8fd9\\u7ec4\\u6263\\u8d39\\u6210\\u529f\\u7684\\u8bf7\\u6c42\\uff0c\\u6309\\u4efb\\u52a1\\u5206\\u7c7b\\u6c47\\u603b\\u3002\\u8bad\\u7ec3\\u5728\\u300c\\u5206\\u7c7b\\u5934\\u300d\\u9875\\u3002',
      sgClassTrafficOpen: '\\u6d41\\u91cf',
      sgTryRules: '\\u8bd5\\u8dd1\\u89c4\\u5219', sgTryPlaceholder: '\\u5199\\u4e00\\u4efd\\u5546\\u4e1a\\u8ba1\\u5212',
      sgTryWorkflow: '\\u5de5\\u4f5c\\u6d41\\u7c7b\\u578b', sgTryPhase: '\\u9636\\u6bb5\\u7c7b\\u578b', sgTryTask: '\\u4efb\\u52a1\\u7c7b\\u578b',
      sgClassTrain: '\\u5206\\u7c7b\\u6d41\\u91cf',
      sgClassTrainHint: '\\u7ec4\\u4e0a\\u53ea\\u770b\\u6263\\u8d39\\u5206\\u7c7b\\u6d41\\u91cf\\u3002\\u8bad\\u7ec3\\u5168\\u5c40\\u4e00\\u4efd\\u5934\\uff0c\\u8bf7\\u5230\\u300c\\u5206\\u7c7b\\u5934\\u300d\\u9875\\u3002',
      sgClassTrainOpen: '\\u6d41\\u91cf', sgClassTrainNeedSave: '\\u5148\\u4fdd\\u5b58\\u8fd9\\u4e2a\\u52a8\\u6001\\u7ec4\\uff0c\\u624d\\u80fd\\u770b\\u5206\\u7c7b\\u6d41\\u91cf\\u3002',
""",
"""      sgClassTraffic: '\\u4e0b\\u6e38\\u6d41\\u91cf', sgClassTrafficHint: '\\u8fd9\\u7ec4\\u6263\\u8d39\\u6210\\u529f\\u7684\\u8bf7\\u6c42\\uff0c\\u6309\\u4efb\\u52a1\\u5206\\u7c7b\\u6c47\\u603b\\u3002\\u8bad\\u7ec3\\u5728\\u300c\\u5206\\u7c7b\\u5934\\u300d\\u9875\\u3002',
      sgClassTrafficOpen: '\\u6d41\\u91cf',
      sgTryRules: '\\u8bd5\\u8dd1\\u89c4\\u5219', sgTryPlaceholder: '\\u5199\\u4e00\\u4efd\\u5546\\u4e1a\\u8ba1\\u5212',
      sgTryWorkflow: '\\u5de5\\u4f5c\\u6d41\\u7c7b\\u578b', sgTryPhase: '\\u9636\\u6bb5\\u7c7b\\u578b', sgTryTask: '\\u4efb\\u52a1\\u7c7b\\u578b',
"""
),
(
"""  function sgGroupDialogAlive(id){
    return sgDialogAlive('group',id)||sgDialogAlive('traffic',id);
  }
""",
"""  function sgTrafficDialogAlive(id){
    return sgDialogAlive('traffic',id);
  }
"""
),
(
"""  function sgCachedHead(id){
    return (window._sgHeadDataId===id && window._sgHeadData) ? window._sgHeadData : null;
  }
""",
""
),
(
"""    if(!g){toast(t('sgFailed'),'error');return;}
    sgMode='traffic';
    sgDraft=sgCloneGroup(g);
""",
"""    if(!g){toast(t('sgFailed'),'error');return;}
    sgDraft=sgCloneGroup(g);
"""
),
(
"""  window.editLLMClassTrain=function(){window.switchLLMSubTab('classHead');};
  window.showLLMGroupEditor=function(){window.showGroupDialog('create');};
""",
"""  window.showLLMGroupEditor=function(){window.showGroupDialog('create');};
"""
),
(
"""      if(seq!==sgTrafficSeq||!sgGroupDialogAlive(id))return;
""",
"""      if(seq!==sgTrafficSeq||!sgTrafficDialogAlive(id))return;
"""
),
(
"""    }catch(e){if(seq===sgTrafficSeq&&sgGroupDialogAlive(id)&&(el=document.getElementById('sgClassTraffic')))el.textContent=e.message||t('sgFailed');}
""",
"""    }catch(e){if(seq===sgTrafficSeq&&sgTrafficDialogAlive(id)&&(el=document.getElementById('sgClassTraffic')))el.textContent=e.message||t('sgFailed');}
"""
),
(
"""if(seq!==sgTrySeq||!sgGroupDialogAlive(id))return;out=document.getElementById('sgTryRunOut');if(out)out.innerHTML=sgFmtTryResult(data);}catch(e){if(seq===sgTrySeq&&sgGroupDialogAlive(id)&&(out=document.getElementById('sgTryRunOut')))out.textContent=e.message||t('sgFailed');}
""",
"""if(seq!==sgTrySeq||!sgTrafficDialogAlive(id))return;out=document.getElementById('sgTryRunOut');if(out)out.innerHTML=sgFmtTryResult(data);}catch(e){if(seq===sgTrySeq&&sgTrafficDialogAlive(id)&&(out=document.getElementById('sgTryRunOut')))out.textContent=e.message||t('sgFailed');}
"""
),
(
"""  window.sgSaveGroup=async function(){
    if(sgSaveBusy)return;
    if(!sgDraft||!sgDraft.id||!sgDraft.name){toast(t('sgIDNameRequired'),'error');return;}
""",
"""  window.sgSaveGroup=async function(){
    if(sgSaveBusy||sgOpenKind!=='group')return;
    if(!sgDraft||!sgDraft.id||!sgDraft.name){toast(t('sgIDNameRequired'),'error');return;}
"""
),
]
for i,(old,new) in enumerate(repls):
    if old not in text:
        raise SystemExit(f"block {i} not found")
    text = text.replace(old, new, 1)
p.write_text(text, encoding="utf-8", newline="\\n")
print("ok")
print("sgClassTrain", text.count("sgClassTrain"))
print("sgMode traffic", "sgMode='traffic'" in text)
print("sgGroupDialogAlive", text.count("sgGroupDialogAlive"))
print("sgTrafficDialogAlive", text.count("sgTrafficDialogAlive"))
print("editLLMClassTrain", "editLLMClassTrain" in text)
print("sgCachedHead", "sgCachedHead" in text)