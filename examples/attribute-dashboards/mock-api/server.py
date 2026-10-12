#!/usr/bin/env python3
"""Minimal stand-in for the DoiT API used by the Attribute dashboards demo.

Serves the four endpoints the Grafana plugin calls, with deterministic sample data
that illustrates runtime cost attribution (allocated vs. unallocated spend, showback
by team, cost per customer). It is a demo fixture, not a reference implementation.
"""
import json
import random
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

DAY = 86400
random.seed(42)

TEAMS = [("Platform", 312118, 400000), ("Payments", 194054, 200000), ("Search", 197330, 240000),
         ("Data", 163540, 180000), ("ML Inference", 110550, 160000), ("Checkout", 98210, 120000),
         ("Growth", 61932, 90000), ("Identity", 48770, 60000), ("Mobile", 41205, 60000),
         ("Analytics", 36910, 50000), ("Notifications", 22480, 40000), ("Billing", 19640, 30000)]
CUSTOMERS = [("Northwind Retail", 80761, 118000, 34), ("Halcyon Labs", 38940, 71500, 71),
             ("Meridian Systems", 59951, 142000, 120), ("Bluefin Analytics", 41704, 124000, 210),
             ("Orbital Health", 10066, 36000, 298), ("Pinecrest Retail", 92410, 265000, 160),
             ("Lumen Logistics", 27300, 64000, 45), ("Granite Financial", 54120, 171000, 330),
             ("Aster Media", 15880, 33000, 88), ("Vela Robotics", 22650, 71000, 240)]

DIMENSIONS = [
    {"id": "cloud_provider", "label": "Cloud", "type": "fixed"},
    {"id": "service_description", "label": "Service", "type": "fixed"},
    {"id": "team", "label": "Team", "type": "identifier"},
    {"id": "customer", "label": "Customer", "type": "identifier"},
    {"id": "feature", "label": "Feature", "type": "identifier"},
    {"id": "workload_namespace", "label": "Workload Namespace", "type": "attribute_workload"},
    {"id": "workload_name", "label": "Workload Name", "type": "attribute_workload"},
]
DIMENSION_VALUES = {
    "team": [t[0] for t in TEAMS],
    "customer": [c[0] for c in CUSTOMERS],
    "feature": ["/api/search", "assistant (LLM)", "/api/reports", "/api/checkout"],
    "cloud_provider": ["amazon-web-services", "google-cloud", "microsoft-azure"],
    "service_description": ["Amazon Elastic Kubernetes Service", "Amazon RDS", "NAT Gateway", "Amazon EC2"],
    "workload_namespace": ["payments", "search", "platform", "data"],
    "workload_name": ["checkout-svc", "search-indexer", "assistant", "etl-batch"],
}

REPORTS = {}
def report(rid, name, fn):
    REPORTS[rid] = {"id": rid, "reportName": name, "owner": "demo@example.com", "type": "custom", "fn": fn}

def day_range(start, end):
    s = datetime.strptime(start, "%Y-%m-%d").replace(tzinfo=timezone.utc)
    e = datetime.strptime(end, "%Y-%m-%d").replace(tzinfo=timezone.utc)
    d = s
    while d <= e:
        yield int(d.timestamp())
        d += timedelta(days=1)

def ts_rows(start, end, series):
    """series: list of (label, fn(day_index, n_days) -> value)."""
    days = list(day_range(start, end)); n = len(days)
    rows = []
    for i, ts in enumerate(days):
        for label, fn in series:
            rows.append([label, round(fn(i, n), 2), ts])
    return rows

# --- Dashboard 1: showback without tagging ---------------------------------
def allocated_vs_unallocated(start, end):
    def attributed(i, n):
        base = 1.10e6 / 30 + random.uniform(-900, 900)
        return base * (1 + 0.004 * i) + (1.46e6 / 30) * ramp(i, n)
    def unallocated(i, n):
        return (1.46e6 / 30) * (1 - ramp(i, n)) * (1 - 0.001 * i) + random.uniform(0, 140)
    return {"schema": [{"name": "allocation_status", "type": "string"}, {"name": "cost", "type": "float"},
                       {"name": "timestamp", "type": "timestamp"}],
            "rows": ts_rows(start, end, [("Attributed", attributed), ("Unallocated", unallocated)])}

def ramp(i, n):
    install = int(n * 0.44)  # sensor installed ~44% into the window
    if i < install: return 0.0
    return min(1.0, 0.35 + 0.65 * (i - install) / 1.5)

def single(name, value, typ="float"):
    return {"schema": [{"name": name, "type": typ}], "rows": [[value]]}

report("allocated-vs-unallocated", "Allocated vs. unallocated spend", allocated_vs_unallocated)
report("allocated-share", "Allocated share of spend (this month)", lambda s, e: single("allocated_share", 0.9984))
report("unallocated-this-month", "Unallocated spend (this month)", lambda s, e: single("unallocated_cost", 4118.27))
report("cost-by-team", "Cost by team (this month)", lambda s, e: {
    "schema": [{"name": "team", "type": "string"}, {"name": "cost", "type": "float"}],
    "rows": [[t, c] for t, c, _ in TEAMS]})
report("team-budget-utilization", "Team budget utilization", lambda s, e: {
    "schema": [{"name": "team", "type": "string"}, {"name": "budget_used", "type": "float"}],
    "rows": [[t, round(c / b, 4)] for t, c, b in TEAMS[:8]]})
report("shared-resources-by-consumer", "Shared resources split by consumer", lambda s, e: {
    "schema": [{"name": "resource", "type": "string"}, {"name": "cost", "type": "float"},
               {"name": "top_consumer", "type": "string"}, {"name": "top_consumer_share", "type": "float"}],
    "rows": [["eks/prod-us-east", 412880, "Payments", 0.47], ["NAT + cross-AZ transfer", 342007, "checkout-svc", 0.35],
             ["rds/orders-db", 96240, "Checkout", 0.61], ["kafka/events", 87115, "Search", 0.38],
             ["ec2/postgres (self-managed)", 72310, "Data", 0.52], ["elasticache/sessions", 33120, "Identity", 0.44]]})
report("unallocated-by-service", "Unallocated spend by service", lambda s, e: {
    "schema": [{"name": "service", "type": "string"}, {"name": "workload", "type": "string"},
               {"name": "cost", "type": "float"}, {"name": "first_seen", "type": "string"}],
    "rows": [["Amazon EC2", "i-0b2f (no owner)", 2860.40, "3 days ago"], ["Amazon S3", "legacy-exports", 1257.87, "41 days ago"]]})

# --- Dashboard 2: cost per customer ----------------------------------------
report("customers-detected", "Customers detected from traffic", lambda s, e: single("customers", 2067, "integer"))
report("customers-below-target", "Customers below 40% margin", lambda s, e: single("customers", 3, "integer"))
report("flagged-before-renewal", "Customers flagged before renewal", lambda s, e: single("customers", 1, "integer"))
report("no-customer-share", "Spend not attributed to a customer", lambda s, e: single("share", 0.018))
report("margin-by-customer", "Cost to serve and margin by customer", lambda s, e: {
    "schema": [{"name": "customer", "type": "string"}, {"name": "cost_to_serve", "type": "float"},
               {"name": "revenue", "type": "float"}, {"name": "margin", "type": "float"}, {"name": "renews_in_days", "type": "integer"}],
    "rows": [[c, cost, rev, round(1 - cost / rev, 3), d] for c, cost, rev, d in CUSTOMERS]})
def cost_to_serve_trend(start, end):
    months = 6
    end_dt = datetime.strptime(end, "%Y-%m-%d").replace(tzinfo=timezone.utc)
    rows = []
    for m in range(months):
        dt = (end_dt.replace(day=1) - timedelta(days=30 * (months - 1 - m))).replace(day=1)
        cost = [62, 64, 66, 70, 74, 80.8][m] * 1000
        rows.append(["Northwind Retail", cost, 118000, int(dt.timestamp())])
    return {"schema": [{"name": "customer", "type": "string"}, {"name": "cost_to_serve", "type": "float"},
                       {"name": "revenue", "type": "float"}, {"name": "timestamp", "type": "timestamp"}], "rows": rows}
report("cost-to-serve-trend", "Cost to serve vs. revenue (Northwind Retail)", cost_to_serve_trend)
report("cost-by-feature", "Cost by feature (Northwind Retail)", lambda s, e: {
    "schema": [{"name": "feature", "type": "string"}, {"name": "cost", "type": "float"}],
    "rows": [["/api/search", 31210], ["assistant (LLM)", 21604], ["/api/reports", 18390], ["/api/checkout", 9557]]})
report("usage-type-split", "Human vs. agent usage (Northwind Retail)", lambda s, e: {
    "schema": [{"name": "usage_type", "type": "string"}, {"name": "cost", "type": "float"}],
    "rows": [["Human", 50072], ["Agent", 30689]]})
report("ai-cost-by-model", "AI cost by model (Northwind Retail)", lambda s, e: {
    "schema": [{"name": "model", "type": "string"}, {"name": "input_tokens", "type": "float"}, {"name": "output_tokens", "type": "float"},
               {"name": "cached_tokens", "type": "float"}, {"name": "cost", "type": "float"}],
    "rows": [["gpt-4.1", 0.62, 0.24, 0.14, 12480], ["claude-sonnet-4.6", 0.58, 0.31, 0.11, 6910], ["claude-opus-5", 0.49, 0.40, 0.11, 2214]]})


class Handler(BaseHTTPRequestHandler):
    def _json(self, status, payload):
        body = json.dumps(payload).encode()
        self.send_response(status); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)

    def do_GET(self):
        url = urlparse(self.path); q = parse_qs(url.query)
        if not self.headers.get("Authorization", "").startswith("Bearer "):
            return self._json(401, {"error": "missing bearer token"})
        if url.path == "/analytics/v1/dimensions":
            return self._json(200, {"dimensions": DIMENSIONS, "pageToken": ""})
        if url.path == "/analytics/v1/dimension":
            did = q.get("id", [""])[0]
            vals = DIMENSION_VALUES.get(did)
            if vals is None: return self._json(404, {"error": "not found"})
            label = next((d["label"] for d in DIMENSIONS if d["id"] == did), did)
            return self._json(200, {"id": did, "label": label, "type": q.get("type", [""])[0],
                                    "values": [{"value": v, "cloud": "amazon-web-services"} for v in vals]})
        if url.path == "/analytics/v1/reports":
            return self._json(200, {"reports": [{k: r[k] for k in ("id", "reportName", "owner", "type")} for r in REPORTS.values()], "pageToken": ""})
        if url.path.startswith("/analytics/v1/reports/"):
            rid = url.path.rsplit("/", 1)[1]
            r = REPORTS.get(rid)
            if not r: return self._json(404, {"error": "report not found"})
            today = datetime.now(timezone.utc).date()
            start = q.get("startDate", [str(today - timedelta(days=89))])[0]
            end = q.get("endDate", [str(today)])[0]
            random.seed(hash(rid) & 0xFFFF)
            return self._json(200, {"id": rid, "reportName": r["reportName"], "urlUI": "", "result": r["fn"](start, end)})
        return self._json(404, {"error": "not found"})

    def do_POST(self):
        if self.path == "/analytics/v1/reports/query":
            length = int(self.headers.get("Content-Length", 0)); self.rfile.read(length)
            today = datetime.now(timezone.utc).date()
            return self._json(200, {"result": REPORTS["cost-by-team"]["fn"](str(today - timedelta(days=29)), str(today))})
        return self._json(404, {"error": "not found"})

    def log_message(self, fmt, *args):
        print("%s %s" % (self.command, self.path), flush=True)


if __name__ == "__main__":
    print("mock DoiT API listening on :8080", flush=True)
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
