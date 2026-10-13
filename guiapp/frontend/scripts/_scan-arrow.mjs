import fs from "fs";
const source = fs.readFileSync("node_modules/mermaid/dist/mermaid.esm.mjs", "utf8");
const index = source.indexOf("function createUserStyles");
console.log(source.slice(index, index + 1800));
