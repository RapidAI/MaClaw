package browser

// pierceFindJS locates nodes through open shadow roots and same-origin iframes.
const pierceFindJS = `function findDeep(root, sel) {
	try { const el = root.querySelector(sel); if (el) return el; } catch (e) {}
	let all = [];
	try { all = root.querySelectorAll('*'); } catch (e) { return null; }
	for (const n of all) {
		if (n.shadowRoot) {
			const found = findDeep(n.shadowRoot, sel);
			if (found) return found;
		}
	}
	return null;
}
function findInFrames(doc, sel) {
	const el = findDeep(doc, sel);
	if (el) return el;
	for (const f of queryIframes(doc)) {
		let child = null;
		try { child = f.contentDocument; } catch (e) {}
		if (!child) continue;
		const found = findInFrames(child, sel);
		if (found) return found;
	}
	return null;
}
function queryAllDeep(root, selector) {
	const out = [];
	function walk(node) {
		if (!node || !node.querySelectorAll) return;
		try { out.push.apply(out, node.querySelectorAll(selector)); } catch (e) {}
		let all = [];
		try { all = node.querySelectorAll('*'); } catch (e) { return; }
		for (const el of all) {
			if (el.shadowRoot) walk(el.shadowRoot);
		}
	}
	walk(root);
	return out;
}
function queryIframes(root) {
	const out = [];
	function walk(node) {
		if (!node) return;
		let children = [];
		try { children = node.children ? Array.from(node.children) : []; } catch (e) { return; }
		for (const el of children) {
			const tag = String(el.tagName || '').toLowerCase();
			if (tag === 'iframe' || tag === 'frame') out.push(el);
			if (el.shadowRoot) walk(el.shadowRoot);
			walk(el);
		}
	}
	walk(root.documentElement || root);
	if (root.shadowRoot) walk(root.shadowRoot);
	return out;
}
function countDeepFrames(doc, sel) {
	let n = queryAllDeep(doc, sel).length;
	for (const f of queryIframes(doc)) {
		let child = null;
		try { child = f.contentDocument; } catch (e) {}
		if (child) n += countDeepFrames(child, sel);
	}
	return n;
}
function normFrameURL(u) { return String(u || '').replace(/\/+$/, ''); }
function matchFrame(f, name, url) {
	if (!name && !url) return false;
	const fname = String(f.name || f.id || '');
	if (name && fname === name) return true;
	if (!url) return false;
	let href = '';
	try { href = f.contentDocument && f.contentDocument.location ? f.contentDocument.location.href : ''; } catch (e) {}
	return normFrameURL(href) === normFrameURL(url) || normFrameURL(f.src) === normFrameURL(url);
}
function findFrameChain(doc, name, url, chain) {
	chain = chain || [];
	if (!name && !url) return {doc: doc, chain: chain};
	for (const f of queryIframes(doc)) {
		const next = chain.concat([f]);
		if (matchFrame(f, name, url)) {
			try { if (f.contentDocument) return {doc: f.contentDocument, chain: next}; } catch (e) {}
		}
		try {
			const child = f.contentDocument;
			if (child) {
				const found = findFrameChain(child, name, url, next);
				if (found) return found;
			}
		} catch (e) {}
	}
	return null;
}
function docAtPath(path) {
	if (!path || !path.length) return null;
	let doc = document;
	const chain = [];
	for (const i of path) {
		const iframes = queryIframes(doc);
		const idx = Number(i);
		if (idx < 0 || idx >= iframes.length) return null;
		const f = iframes[idx];
		chain.push(f);
		try { doc = f.contentDocument; } catch (e) { return null; }
		if (!doc) return null;
	}
	return {doc: doc, chain: chain};
}
function findScopedLocated(sel, name, url, path) {
	if ((path && path.length) || name || url) {
		let scoped = null;
		if (path && path.length) scoped = docAtPath(path);
		if (!scoped && (name || url)) scoped = findFrameChain(document, name, url, []);
		if (!scoped || !scoped.doc) return null;
		const el = findDeep(scoped.doc, sel);
		if (!el) return null;
		return {el: el, chain: scoped.chain};
	}
	function walk(doc, chain) {
		const el = findDeep(doc, sel);
		if (el) return {el: el, chain: chain};
		for (const f of queryIframes(doc)) {
			let child = null;
			try { child = f.contentDocument; } catch (e) {}
			if (!child) continue;
			const found = walk(child, chain.concat([f]));
			if (found) return found;
		}
		return null;
	}
	return walk(document, []);
}
function findScoped(sel, name, url, path) {
	const found = findScopedLocated(sel, name, url, path);
	return found ? found.el : null;
}
function countScoped(sel, name, url, path) {
	if ((path && path.length) || name || url) {
		let scoped = null;
		if (path && path.length) scoped = docAtPath(path);
		if (!scoped && (name || url)) scoped = findFrameChain(document, name, url, []);
		if (!scoped || !scoped.doc) return 0;
		return queryAllDeep(scoped.doc, sel).length;
	}
	return countDeepFrames(document, sel);
}
function pageTextFrom(doc) {
	const parts = [];
	function collect(root) {
		if (!root) return;
		try {
			if (root.body) parts.push(String(root.body.innerText || root.body.textContent || ''));
			else parts.push(String(root.innerText || root.textContent || ''));
		} catch (e) {}
		let all = [];
		try { all = root.querySelectorAll('*'); } catch (e) { return; }
		for (const el of all) {
			if (el.shadowRoot) collect(el.shadowRoot);
		}
	}
	function walk(d) {
		if (!d) return;
		collect(d);
		for (const f of queryIframes(d)) {
			try { if (f.contentDocument) walk(f.contentDocument); } catch (e) {}
		}
	}
	walk(doc || document);
	return String(parts.join(' ')).replace(/\s+/g, ' ').trim();
}`
