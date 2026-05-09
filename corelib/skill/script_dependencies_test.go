package skill

import "testing"

func TestExtractScriptRequiresPythonKnownThirdParty(t *testing.T) {
	req := ExtractScriptRequires(`
import os
import requests
from bs4 import BeautifulSoup
import yaml
from local_helper import run
`, "python")

	if req == nil {
		t.Fatal("ExtractScriptRequires() = nil, want Python requirements")
	}
	want := []string{"requests", "beautifulsoup4", "PyYAML"}
	if len(req.Python) != len(want) {
		t.Fatalf("Python requirements = %#v, want %#v", req.Python, want)
	}
	for i := range want {
		if req.Python[i] != want[i] {
			t.Fatalf("Python requirements[%d] = %q, want %q (all=%#v)", i, req.Python[i], want[i], req.Python)
		}
	}
	if len(req.Node) != 0 {
		t.Fatalf("Node requirements = %#v, want none", req.Node)
	}
}

func TestExtractScriptRequiresPythonMultiImportAliases(t *testing.T) {
	req := ExtractScriptRequires(`
import os, requests as rq, pandas as pd
import numpy.typing as npt, local_helper
from PIL import Image
from .relative import helper
`, "py")

	if req == nil {
		t.Fatal("ExtractScriptRequires() = nil, want Python requirements")
	}
	want := []string{"requests", "pandas", "numpy", "Pillow"}
	if len(req.Python) != len(want) {
		t.Fatalf("Python requirements = %#v, want %#v", req.Python, want)
	}
	for i := range want {
		if req.Python[i] != want[i] {
			t.Fatalf("Python requirements[%d] = %q, want %q (all=%#v)", i, req.Python[i], want[i], req.Python)
		}
	}
}

func TestExtractScriptRequiresNodeBarePackages(t *testing.T) {
	req := ExtractScriptRequires(`
const fs = require("fs")
const { program } = require("commander")
import puppeteer from "puppeteer"
import helper from "./helper.js"
import scoped from "@scope/pkg/subpath"
import path from "node:path"
`, "node")

	if req == nil {
		t.Fatal("ExtractScriptRequires() = nil, want Node requirements")
	}
	want := []string{"commander", "puppeteer", "@scope/pkg"}
	if len(req.Node) != len(want) {
		t.Fatalf("Node requirements = %#v, want %#v", req.Node, want)
	}
	for i := range want {
		if req.Node[i] != want[i] {
			t.Fatalf("Node requirements[%d] = %q, want %q (all=%#v)", i, req.Node[i], want[i], req.Node)
		}
	}
	if len(req.Python) != 0 {
		t.Fatalf("Python requirements = %#v, want none", req.Python)
	}
}

func TestExtractScriptRequiresNodeDynamicImports(t *testing.T) {
	req := ExtractScriptRequires(`
const mod = await import("sharp")
await import("./local.js")
await import("node:fs")
`, "js")

	if req == nil {
		t.Fatal("ExtractScriptRequires() = nil, want Node requirements")
	}
	if len(req.Node) != 1 || req.Node[0] != "sharp" {
		t.Fatalf("Node requirements = %#v, want [sharp]", req.Node)
	}
}

func TestExtractScriptRequiresNodePreservesSourceOrder(t *testing.T) {
	req := ExtractScriptRequires(`
import puppeteer from "puppeteer"
const { program } = require("commander")
const mod = await import("sharp")
`, "node")

	if req == nil {
		t.Fatal("ExtractScriptRequires() = nil, want Node requirements")
	}
	want := []string{"puppeteer", "commander", "sharp"}
	if len(req.Node) != len(want) {
		t.Fatalf("Node requirements = %#v, want %#v", req.Node, want)
	}
	for i := range want {
		if req.Node[i] != want[i] {
			t.Fatalf("Node requirements[%d] = %q, want %q (all=%#v)", i, req.Node[i], want[i], req.Node)
		}
	}
}
