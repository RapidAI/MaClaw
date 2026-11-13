const katex = require('katex');
const BS = String.fromCharCode(92);
const ok = (s) => { try { katex.renderToString(s, { throwOnError: true, strict: 'ignore' }); return true; } catch (e) { return false; } };

// command -> test expression
const tests = {
  defs: 'x', impliedby: 'a' + BS + 'impliedby b', implies: 'a' + BS + 'implies b', iff: 'a' + BS + 'iff b',
  bm: BS + 'bm{x}', boldsymbol: BS + 'boldsymbol{x}', mathinner: BS + 'mathinner{x}', mathbin: BS + 'mathbin{+}', mathord: BS + 'mathord{x}',
  smallint: BS + 'smallint', dagger: BS + 'dagger', ddagger: BS + 'ddagger', therefore: BS + 'therefore', because: BS + 'because',
  triangleq: 'a' + BS + 'triangleq b', colon: 'a' + BS + 'colon b', gets: 'a' + BS + 'gets b', owns: 'a' + BS + 'owns b',
  doteq: 'a' + BS + 'doteq b', asymp: 'a' + BS + 'asymp b', hslash: BS + 'hslash', backprime: BS + 'backprime',
  checkmark: BS + 'checkmark', oiint: BS + 'oiint', bigsqcup: BS + 'bigsqcup', mathscr: BS + 'mathscr{A}',
  dotsc: 'a' + BS + 'dotsc', dotsi: 'a' + BS + 'dotsi', xRightarrow: BS + 'xRightarrow{y}', xrightarrow: BS + 'xrightarrow{y}',
  substack: BS + 'substack{a}', middle: BS + 'middle|', dot: BS + 'dot{x}', ddot: BS + 'ddot{x}', dddot: BS + 'dddot{x}',
  breve: BS + 'breve{x}', mathring: BS + 'mathring{x}', fbox: BS + 'fbox{x}', hphantom: BS + 'hphantom{x}', vphantom: BS + 'vphantom{x}',
  mkern: BS + 'mkern 2mu', medspace: BS + 'medspace', thickspace: BS + 'thickspace', cfrac: BS + 'cfrac{a}{b}',
  dbinom: BS + 'dbinom{a}{b}', tbinom: BS + 'tbinom{a}{b}', biguplus: BS + 'biguplus', nexists: BS + 'nexists',
  lnot: BS + 'lnot', smallsetminus: 'a' + BS + 'smallsetminus b', nparallel: 'a' + BS + 'nparallel b', dashv: 'a' + BS + 'dashv b',
  measuredangle: BS + 'measuredangle', triangledown: BS + 'triangledown', blacksquare: BS + 'blacksquare', lozenge: BS + 'lozenge',
  beth: BS + 'beth', gimel: BS + 'gimel', iint: BS + 'iint', iiint: BS + 'iiint', mathop: BS + 'mathop{x}',
  kern: BS + 'kern 2pt', hspace: BS + 'hspace{2mm}', stackrel: BS + 'stackrel{a}{b}', overset: BS + 'overset{a}{b}',
  underset: BS + 'underset{a}{b}', operatorname: BS + 'operatorname{arg}', boxed: BS + 'boxed{x}', phantom: BS + 'phantom{x}',
  displaystyle: BS + 'displaystyle x', textstyle: BS + 'textstyle x', scriptstyle: BS + 'scriptstyle x',
  limits: BS + 'sum' + BS + 'limits', nolimits: BS + 'sum' + BS + 'nolimits', pmod: 'a' + BS + 'pmod{b}', bmod: 'a' + BS + 'bmod b',
  mod: 'a' + BS + 'mod{b}', ker: BS + 'ker', hom: BS + 'hom', gcd: BS + 'gcd', Pr: BS + 'Pr', lg: BS + 'lg', arg: BS + 'arg',
  deg: BS + 'deg', dim: BS + 'dim', limsup: BS + 'limsup', liminf: BS + 'liminf', nearrow: BS + 'nearrow', searrow: BS + 'searrow',
  swarrow: BS + 'swarrow', nwarrow: BS + 'nwarrow', hookrightarrow: 'a' + BS + 'hookrightarrow b', hookleftarrow: 'a' + BS + 'hookleftarrow b',
  rightharpoonup: 'a' + BS + 'rightharpoonup b', leftharpoonup: 'a' + BS + 'leftharpoonup b', rightleftharpoons: 'a' + BS + 'rightleftharpoons b',
  longmapsto: 'a' + BS + 'longmapsto b', updownarrow: BS + 'updownarrow', varpi: BS + 'varpi', varrho: BS + 'varrho',
  varsigma: BS + 'varsigma', upsilon: BS + 'upsilon', vartheta: BS + 'vartheta', imath: BS + 'imath', jmath: BS + 'jmath',
  wp: BS + 'wp', aleph: BS + 'aleph', Re: BS + 'Re', Im: BS + 'Im', ell: BS + 'ell', hbar: BS + 'hbar', nabla: BS + 'nabla',
  partial: BS + 'partial', infty: BS + 'infty', surd: BS + 'surd', top: BS + 'top', bot: BS + 'bot', angle: BS + 'angle',
  square: BS + 'square', diamond: BS + 'diamond', neg: BS + 'neg', forall: BS + 'forall', exists: BS + 'exists',
  emptyset: BS + 'emptyset', varnothing: BS + 'varnothing', models: 'a' + BS + 'models b', vdash: 'a' + BS + 'vdash b',
  perp: 'a' + BS + 'perp b', parallel: 'a' + BS + 'parallel b', nmid: 'a' + BS + 'nmid b', ni: 'a' + BS + 'ni b',
  notin: 'a' + BS + 'notin b', prec: 'a' + BS + 'prec b', succ: 'a' + BS + 'succ b', preceq: 'a' + BS + 'preceq b',
  succeq: 'a' + BS + 'succeq b', ll: 'a' + BS + 'll b', gg: 'a' + BS + 'gg b', subset: 'a' + BS + 'subset b',
  supset: 'a' + BS + 'supset b', subseteq: 'a' + BS + 'subseteq b', supseteq: 'a' + BS + 'supseteq b', cup: 'a' + BS + 'cup b',
  cap: 'a' + BS + 'cap b', setminus: 'a' + BS + 'setminus b', circ: 'a' + BS + 'circ b', bullet: 'a' + BS + 'bullet b',
  ast: 'a' + BS + 'ast b', star: 'a' + BS + 'star b', oplus: 'a' + BS + 'oplus b', ominus: 'a' + BS + 'ominus b',
  otimes: 'a' + BS + 'otimes b', oslash: 'a' + BS + 'oslash b', odot: 'a' + BS + 'odot b', amalg: 'a' + BS + 'amalg b',
  mp: 'a' + BS + 'mp b', pm: 'a' + BS + 'pm b', times: 'a' + BS + 'times b', div: 'a' + BS + 'div b', cdot: 'a' + BS + 'cdot b',
  cdots: 'a' + BS + 'cdots', ldots: 'a' + BS + 'ldots', vdots: BS + 'vdots', ddots: BS + 'ddots', dots: 'a' + BS + 'dots',
  dotsb: 'a' + BS + 'dotsb', dotsc: 'a' + BS + 'dotsc', dotsm: 'a' + BS + 'dotsm', dotsi: 'a' + BS + 'dotsi',
  xleftarrow: BS + 'xleftarrow{y}', xLeftarrow: BS + 'xLeftarrow{y}', xrightleftharpoons: BS + 'xrightleftharpoons{y}',
};
const bad = [];
for (const [c, s] of Object.entries(tests)) { if (!ok(s)) bad.push(c); }
console.log('unsupported cmds:', bad.join(','));

const envTests = {
  aligned: BS + 'begin{aligned} a &= b ' + BS + BS + ' c &= d ' + BS + 'end{aligned}',
  align: BS + 'begin{align} a &= b ' + BS + 'end{align}',
  'align*': BS + 'begin{align*} a &= b ' + BS + 'end{align*}',
  gather: BS + 'begin{gather} a = b ' + BS + 'end{gather}',
  gathered: BS + 'begin{gathered} a = b ' + BS + BS + ' c = d ' + BS + 'end{gathered}',
  multline: BS + 'begin{multline} a = b ' + BS + 'end{multline}',
  split: BS + 'begin{split} a &= b ' + BS + 'end{split}',
  cases: BS + 'begin{cases} a & x > 0 ' + BS + BS + ' b & x < 0 ' + BS + 'end{cases}',
  dcases: BS + 'begin{dcases} a & x > 0 ' + BS + 'end{dcases}',
  rcases: BS + 'begin{rcases} a & x > 0 ' + BS + 'end{rcases}',
  matrix: BS + 'begin{matrix} a & b ' + BS + 'end{matrix}',
  pmatrix: BS + 'begin{pmatrix} a & b ' + BS + 'end{pmatrix}',
  bmatrix: BS + 'begin{bmatrix} a & b ' + BS + 'end{bmatrix}',
  Bmatrix: BS + 'begin{Bmatrix} a & b ' + BS + 'end{Bmatrix}',
  vmatrix: BS + 'begin{vmatrix} a & b ' + BS + 'end{vmatrix}',
  Vmatrix: BS + 'begin{Vmatrix} a & b ' + BS + 'end{Vmatrix}',
  'matrix*': BS + 'begin{matrix*}[r] a & b ' + BS + 'end{matrix*}',
  smallmatrix: BS + 'begin{smallmatrix} a & b ' + BS + 'end{smallmatrix}',
  array: BS + 'begin{array}{cc} a & b ' + BS + 'end{array}',
  subarray: BS + 'begin{subarray}{c} a ' + BS + 'end{subarray}',
  eqnarray: BS + 'begin{eqnarray} a &=& b ' + BS + 'end{eqnarray}',
  equation: BS + 'begin{equation} a = b ' + BS + 'end{equation}',
  'equation*': BS + 'begin{equation*} a = b ' + BS + 'end{equation*}',
  alignat: BS + 'begin{alignat}{2} a &= b ' + BS + 'end{alignat}',
  alignedat: BS + 'begin{alignedat}{2} a &= b ' + BS + 'end{alignedat}',
  darray: BS + 'begin{darray}{cc} a & b ' + BS + 'end{darray}',
  CD: BS + 'begin{CD} A @>f>> B ' + BS + 'end{CD}',
};
const badEnv = [];
for (const [e, s] of Object.entries(envTests)) { if (!ok(s)) badEnv.push(e); }
console.log('unsupported envs:', badEnv.join(','));
