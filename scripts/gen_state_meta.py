#!/usr/bin/env python3

import json
import re
import xml.etree.ElementTree as ET
from html.parser import HTMLParser
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DATA_DIR = ROOT / "data"

CAPITALS = {
    "AL": {"name": "Alabama", "capital": "Montgomery"},
    "AK": {"name": "Alaska", "capital": "Juneau"},
    "AZ": {"name": "Arizona", "capital": "Phoenix"},
    "AR": {"name": "Arkansas", "capital": "Little Rock"},
    "CA": {"name": "California", "capital": "Sacramento"},
    "CO": {"name": "Colorado", "capital": "Denver"},
    "CT": {"name": "Connecticut", "capital": "Hartford"},
    "DE": {"name": "Delaware", "capital": "Dover"},
    "FL": {"name": "Florida", "capital": "Tallahassee"},
    "GA": {"name": "Georgia", "capital": "Atlanta"},
    "HI": {"name": "Hawaii", "capital": "Honolulu"},
    "ID": {"name": "Idaho", "capital": "Boise"},
    "IL": {"name": "Illinois", "capital": "Springfield"},
    "IN": {"name": "Indiana", "capital": "Indianapolis"},
    "IA": {"name": "Iowa", "capital": "Des Moines"},
    "KS": {"name": "Kansas", "capital": "Topeka"},
    "KY": {"name": "Kentucky", "capital": "Frankfort"},
    "LA": {"name": "Louisiana", "capital": "Baton Rouge"},
    "ME": {"name": "Maine", "capital": "Augusta"},
    "MD": {"name": "Maryland", "capital": "Annapolis"},
    "MA": {"name": "Massachusetts", "capital": "Boston"},
    "MI": {"name": "Michigan", "capital": "Lansing"},
    "MN": {"name": "Minnesota", "capital": "Saint Paul"},
    "MS": {"name": "Mississippi", "capital": "Jackson"},
    "MO": {"name": "Missouri", "capital": "Jefferson City"},
    "MT": {"name": "Montana", "capital": "Helena"},
    "NE": {"name": "Nebraska", "capital": "Lincoln"},
    "NV": {"name": "Nevada", "capital": "Carson City"},
    "NH": {"name": "New Hampshire", "capital": "Concord"},
    "NJ": {"name": "New Jersey", "capital": "Trenton"},
    "NM": {"name": "New Mexico", "capital": "Santa Fe"},
    "NY": {"name": "New York", "capital": "Albany"},
    "NC": {"name": "North Carolina", "capital": "Raleigh"},
    "ND": {"name": "North Dakota", "capital": "Bismarck"},
    "OH": {"name": "Ohio", "capital": "Columbus"},
    "OK": {"name": "Oklahoma", "capital": "Oklahoma City"},
    "OR": {"name": "Oregon", "capital": "Salem"},
    "PA": {"name": "Pennsylvania", "capital": "Harrisburg"},
    "RI": {"name": "Rhode Island", "capital": "Providence"},
    "SC": {"name": "South Carolina", "capital": "Columbia"},
    "SD": {"name": "South Dakota", "capital": "Pierre"},
    "TN": {"name": "Tennessee", "capital": "Nashville"},
    "TX": {"name": "Texas", "capital": "Austin"},
    "UT": {"name": "Utah", "capital": "Salt Lake City"},
    "VT": {"name": "Vermont", "capital": "Montpelier"},
    "VA": {"name": "Virginia", "capital": "Richmond"},
    "WA": {"name": "Washington", "capital": "Olympia"},
    "WV": {"name": "West Virginia", "capital": "Charleston"},
    "WI": {"name": "Wisconsin", "capital": "Madison"},
    "WY": {"name": "Wyoming", "capital": "Cheyenne"},
}

STATE_NAME_TO_CODE = {item["name"]: code for code, item in CAPITALS.items()}


class GovernorParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.in_state = False
        self.capture_div = False
        self.current_state = None
        self.governors = {}
        self._buffer = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        cls = attrs.get("class", "")
        if tag == "small" and "state" in cls:
            self.in_state = True
        elif tag == "div" and "current-governors__item__link" in cls:
            self.capture_div = True
            self._buffer = []

    def handle_endtag(self, tag):
        if tag == "small":
            self.in_state = False
        elif tag == "div" and self.capture_div:
            text = " ".join(" ".join(self._buffer).split()).strip()
            if self.current_state and text:
                cleaned = re.sub(rf"^{re.escape(self.current_state)}\s*", "", text).strip()
                cleaned = re.sub(r"^Gov\.\s*", "", cleaned).strip()
                if cleaned:
                    self.governors[self.current_state] = cleaned
            self.current_state = None
            self.capture_div = False
            self._buffer = []

    def handle_data(self, data):
        text = " ".join(data.split()).strip()
        if not text:
            return
        if self.in_state:
            self.current_state = text
            return
        if self.capture_div:
            self._buffer.append(text)


def parse_senators(xml_path: Path):
    root = ET.fromstring(xml_path.read_text())
    senators = {}
    for member in root.findall("member"):
        state = member.findtext("state", "").strip()
        first = member.findtext("first_name", "").strip()
        last = member.findtext("last_name", "").strip()
        if not state or not first or not last:
            continue
        senators.setdefault(state, []).append(f"{first} {last}")
    return senators


def parse_governors(html_path: Path):
    parser = GovernorParser()
    parser.feed(html_path.read_text())
    governors = {}
    for state_name, governor in parser.governors.items():
        code = STATE_NAME_TO_CODE.get(state_name)
        if code:
            governors[code] = governor
    return governors


def main():
    senators = parse_senators(Path("/tmp/senators.xml"))
    governors = parse_governors(Path("/tmp/nga-governors.html"))

    payload = {}
    for code, details in CAPITALS.items():
        payload[code] = {
            "state": details["name"],
            "capital": details["capital"],
            "governor": governors.get(code, ""),
            "senators": sorted(senators.get(code, [])),
        }

    DATA_DIR.mkdir(exist_ok=True)
    (DATA_DIR / "state_meta.json").write_text(json.dumps(payload, indent=2))
    print("states", len(payload))


if __name__ == "__main__":
    main()
