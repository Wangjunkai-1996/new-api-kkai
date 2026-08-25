#!/usr/bin/env python3

import argparse
import hashlib
import html
import json
import re
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET
from html.parser import HTMLParser
from pathlib import Path


ORIGIN = "https://api.kkrich.ltd"
BASE_PATH = "/docs/"
CURATED_PATHS = {
    "apps/seedance.md",
    "api/video-generation.md",
    "support/seedance-video.md",
}


def fetch(url: str) -> bytes:
    request = urllib.request.Request(
        url,
        headers={"User-Agent": "KKRICH-docs-source-recovery/1.0"},
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        return response.read()


class VpDocExtractor(HTMLParser):
    VOID_TAGS = {
        "area",
        "base",
        "br",
        "col",
        "embed",
        "hr",
        "img",
        "input",
        "link",
        "meta",
        "param",
        "source",
        "track",
        "wbr",
    }

    def __init__(self) -> None:
        super().__init__(convert_charrefs=False)
        self.capturing = False
        self.depth = 0
        self.parts: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        classes = next((value or "" for key, value in attrs if key == "class"), "")
        if not self.capturing and tag == "div" and "vp-doc" in classes.split():
            self.capturing = True
            self.depth = 1
            return
        if self.capturing:
            self.parts.append(self.get_starttag_text())
            if tag not in self.VOID_TAGS:
                self.depth += 1

    def handle_startendtag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if self.capturing:
            self.parts.append(self.get_starttag_text())

    def handle_endtag(self, tag: str) -> None:
        if not self.capturing:
            return
        self.depth -= 1
        if self.depth == 0:
            self.capturing = False
            return
        self.parts.append(f"</{tag}>")

    def handle_data(self, data: str) -> None:
        if self.capturing:
            self.parts.append(data)

    def handle_entityref(self, name: str) -> None:
        if self.capturing:
            self.parts.append(f"&{name};")

    def handle_charref(self, name: str) -> None:
        if self.capturing:
            self.parts.append(f"&#{name};")

    def handle_comment(self, data: str) -> None:
        if self.capturing:
            self.parts.append(f"<!--{data}-->")


def route_to_source(url: str) -> str:
    path = urllib.parse.urlparse(url).path
    if not path.startswith(BASE_PATH):
        raise ValueError(f"unexpected documentation URL: {url}")
    relative = path[len(BASE_PATH) :].strip("/")
    if not relative:
        return "index.md"
    if path.endswith("/"):
        return f"{relative}/index.md"
    return f"{relative}.md"


def extract_meta(page: str, name: str) -> str:
    match = re.search(
        rf'<meta\s+name="{re.escape(name)}"\s+content="([^"]*)"',
        page,
        re.IGNORECASE,
    )
    return html.unescape(match.group(1)) if match else ""


def extract_title(page: str) -> str:
    match = re.search(r"<title>(.*?)</title>", page, re.IGNORECASE | re.DOTALL)
    if not match:
        return "KKRICH 文档"
    title = html.unescape(match.group(1)).strip()
    return re.sub(r"\s*\|\s*KKRICH 文档$", "", title)


def extract_page_class(page: str) -> str | None:
    match = re.search(r'<div class="Layout\s+([^"]+)"', page)
    if not match:
        return None
    classes = [value for value in match.group(1).split() if value != "Layout"]
    return " ".join(classes) or None


def recover_page(url: str, page_bytes: bytes, source_path: Path) -> dict[str, str]:
    page = page_bytes.decode("utf-8")
    extractor = VpDocExtractor()
    extractor.feed(page)
    body = "".join(extractor.parts).strip()
    if not body:
        raise RuntimeError(f"could not locate .vp-doc content in {url}")
    if re.search(r"<script\b", body, re.IGNORECASE):
        raise RuntimeError(f"refusing to recover script content from {url}")
    # VitePress adds BASE_PATH while building. Production HTML already contains
    # that prefix, so remove it from public asset references in recovered source.
    body = re.sub(
        r'((?:src|href)=")/docs/(images|downloads)/',
        r'\1/\2/',
        body,
    )

    title = extract_title(page)
    description = extract_meta(page, "description")
    page_class = extract_page_class(page)
    frontmatter = [
        "---",
        f"title: {json.dumps(title, ensure_ascii=False)}",
        f"description: {json.dumps(description, ensure_ascii=False)}",
        "outline: [2, 3]",
    ]
    if page_class:
        frontmatter.append(f"pageClass: {json.dumps(page_class, ensure_ascii=False)}")
    frontmatter.extend(["---", ""])
    recovered = "\n".join(frontmatter)
    recovered += "<!-- Recovered from the public production rendering on 2026-08-25. -->\n"
    recovered += '<div class="kkr-recovered-page" v-pre>\n'
    recovered += body
    recovered += "\n</div>\n"
    source_path.parent.mkdir(parents=True, exist_ok=True)
    source_path.write_text(recovered, encoding="utf-8")
    return {
        "url": url,
        "source": str(source_path),
        "sha256": hashlib.sha256(page_bytes).hexdigest(),
    }


def download_public_asset(url: str, public_dir: Path) -> dict[str, str]:
    path = urllib.parse.urlparse(url).path
    if not path.startswith(BASE_PATH):
        raise ValueError(f"unexpected asset URL: {url}")
    relative = path[len(BASE_PATH) :]
    if not relative or relative.startswith("assets/"):
        raise ValueError(f"refusing generated or empty asset path: {url}")
    destination = public_dir / relative
    payload = fetch(url)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_bytes(payload)
    return {
        "url": url,
        "source": str(destination),
        "sha256": hashlib.sha256(payload).hexdigest(),
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--include-curated", action="store_true")
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    docs_dir = root / "docs"
    public_dir = docs_dir / "public"
    sitemap = fetch(f"{ORIGIN}{BASE_PATH}sitemap.xml")
    xml = ET.fromstring(sitemap)
    namespace = {"s": "http://www.sitemaps.org/schemas/sitemap/0.9"}
    urls = [node.text for node in xml.findall("s:url/s:loc", namespace) if node.text]
    manifest: dict[str, list[dict[str, str]]] = {"pages": [], "assets": []}
    asset_urls = {
        f"{ORIGIN}{BASE_PATH}favicon.svg",
        f"{ORIGIN}{BASE_PATH}logo.svg",
        f"{ORIGIN}{BASE_PATH}og.svg",
        f"{ORIGIN}{BASE_PATH}robots.txt",
    }

    for url in urls:
        relative = route_to_source(url)
        page_bytes = fetch(url)
        page = page_bytes.decode("utf-8")
        for match in re.finditer(r'(?:src|href)="(/docs/(?:images|downloads)/[^"#?]+)', page):
            asset_urls.add(urllib.parse.urljoin(ORIGIN, match.group(1)))
        if relative in CURATED_PATHS and not args.include_curated:
            continue
        manifest["pages"].append(recover_page(url, page_bytes, docs_dir / relative))

    for url in sorted(asset_urls):
        manifest["assets"].append(download_public_asset(url, public_dir))

    stylesheet_page = fetch(f"{ORIGIN}{BASE_PATH}").decode("utf-8")
    stylesheet_match = re.search(r'href="(/docs/assets/style\.[^"]+\.css)"', stylesheet_page)
    if not stylesheet_match:
        raise RuntimeError("could not locate production stylesheet")
    stylesheet_url = urllib.parse.urljoin(ORIGIN, stylesheet_match.group(1))
    stylesheet = fetch(stylesheet_url).decode("utf-8")
    font_names = set(re.findall(r"(?:\./)?(inter-[^)\"']+\.woff2)", stylesheet))
    for font_name in font_names:
        font_url = f"{ORIGIN}{BASE_PATH}assets/{font_name}"
        payload = fetch(font_url)
        destination = public_dir / "assets" / font_name
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(payload)
        manifest["assets"].append(
            {
                "url": font_url,
                "source": str(destination),
                "sha256": hashlib.sha256(payload).hexdigest(),
            }
        )
        stylesheet = stylesheet.replace(
            f"./{font_name}",
            f"/assets/{font_name}",
        )
    production_css = docs_dir / ".vitepress" / "theme" / "production.css"
    production_css.parent.mkdir(parents=True, exist_ok=True)
    production_css.write_text(stylesheet, encoding="utf-8")

    manifest_path = root / "recovery-manifest.json"
    manifest_path.write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    print(
        f"Recovered {len(manifest['pages'])} pages and "
        f"{len(manifest['assets'])} assets into {root}"
    )


if __name__ == "__main__":
    main()
