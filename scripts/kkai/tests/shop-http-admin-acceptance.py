#!/usr/bin/env python3
"""Actual HTTP inventory import and manual delivery acceptance.

Start TestShopLiveHTTPServer first; no application API is mocked.
The sole positional argument is its private fixture directory.
This script creates synthetic business records and never accepts production URLs.
"""
import argparse
import http.cookiejar
import ipaddress
import json
import os
import pathlib
import socket
import ssl
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


parser = argparse.ArgumentParser(description='Exercise the real shop HTTP fixture with synthetic local data only.')
parser.add_argument('fixture_dir', type=pathlib.Path, help='Private directory created for TestShopLiveHTTPServer; must contain credentials.json.')
args = parser.parse_args()
if not __debug__:
    parser.error('run without -O so acceptance assertions remain enabled')
P = args.fixture_dir.resolve(strict=True)
if not P.is_dir() or P.stat().st_mode & 0o077:
    parser.error('fixture directory must be private (mode 0700)')
credential_file = P / 'credentials.json'
if credential_file.is_symlink() or not credential_file.is_file() or credential_file.stat().st_mode & 0o077:
    parser.error('credentials.json must be a private regular file (mode 0600)')
os.umask(0o077)
C = json.loads(credential_file.read_text())
BASE = 'http://localhost:3000'
if C.get('backend_url') != BASE or C.get('frontend_url') != 'http://localhost:5178':
    parser.error('only the localhost shop fixture is accepted')
gateway = urllib.parse.urlsplit(C['gateway_url'])
if gateway.scheme != 'https' or gateway.hostname not in {'localhost', '127.0.0.1', '::1'} or (not gateway.port) or (gateway.username is not None) or (gateway.password is not None) or (gateway.path not in {'', '/'}) or gateway.query or gateway.fragment:
    parser.error('only a loopback TLS simulator gateway is accepted')
for host in {'localhost', gateway.hostname}:
    if not all((ipaddress.ip_address(address[4][0]).is_loopback for address in socket.getaddrinfo(host, None))):
        parser.error('fixture hosts must resolve only to loopback addresses')

class NoRedirect(urllib.request.HTTPRedirectHandler):

    def redirect_request(self, *args, **kwargs):
        return None


clients = {}
tokens = {}
results = []

def check(label, condition):
    assert condition, label
    results.append(label)
    print('PASS:', label, flush=True)

def call(who, method, path, body=None, key=None, proof=None, expect=(200, 201, 202)):
    headers = {'Content-Type': 'application/json'}
    if who and who in tokens:
        headers['Authorization'] = 'Bearer ' + tokens[who]
    if key:
        headers['Idempotency-Key'] = key
    if proof:
        headers['X-Security-Proof'] = proof
    request = urllib.request.Request(BASE + path, data=None if body is None else json.dumps(body).encode(), headers=headers, method=method)
    try:
        r = clients.get(who, urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())).open(request, timeout=10)
    except urllib.error.HTTPError as e:
        r = e
    data = json.load(r)
    assert r.status in expect, (method, path, r.status)
    if r.status < 400:
        assert data.get('success'), (method, path)
    return (data.get('data'), data, r.headers)


for who in ['buyer', 'admin', 'other_buyer']:
    clients[who] = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect(), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    d, _, _ = call(who, 'POST', '/api/user/login', {k: C[who][k] for k in ['username', 'password']})
    tokens[who] = d['access_token']
check('real password login for three isolated users', len(tokens) == 3)
d, _, _ = call('buyer', 'POST', '/api/user/auth/refresh', {})
tokens['buyer'] = d['access_token']
check('real session cookie refresh', bool(tokens['buyer']))

def proof(scope, target, version, key, action, details):
    context = {'action': action, 'target_id': target, 'expected_version': version, 'idempotency_key': key, 'details': details}
    data, _, _ = call('admin', 'POST', '/api/verify', {'method': 'password', 'password': C['admin']['password'], 'scope': scope, 'context': context})
    return data['proof_token']
product, _, _ = call('admin', 'GET', '/api/admin/shop/products/' + C['automatic_product_id'])
identity = 'http-import-' + str(uuid.uuid4()) + '@example.invalid'
body = {'product_id': product['id'], 'product_version': product['version'], 'template_version': product['template_version'], 'rows': [{'identity': identity, 'secret_fields': [{'key': 'account', 'value': identity}, {'key': 'password', 'value': 'synthetic-import-secret'}]}]}
preview, _, _ = call('admin', 'POST', '/api/admin/shop/inventory/import-preview', body)
check('actual HTTP import preview accepts one synthetic credential', preview['valid_count'] == 1 and preview['invalid_count'] == 0)
key = str(uuid.uuid4())
commit = {**body, 'preview_digest': preview['preview_digest']}
imported, _, _ = call('admin', 'POST', '/api/admin/shop/inventory/import-commit', commit, key)
replayed, _, _ = call('admin', 'POST', '/api/admin/shop/inventory/import-commit', commit, key)
check('stock import commit is idempotent', imported['inserted_count'] == 1 and replayed['batch_id'] == imported['batch_id'])
recovery, _, _ = call('admin', 'GET', '/api/admin/shop/inventory/import-requests/' + key + '?product_id=' + product['id'])
check('stock import recovers original batch', recovery['status'] == 'recorded' and recovery['batch_id'] == imported['batch_id'])
preview, _, _ = call('admin', 'POST', '/api/admin/shop/inventory/import-preview', body)
check('same stock identity is recognized as duplicate', preview['duplicate_count'] == 1 and preview['valid_count'] == 0)
inventory, _, _ = call('admin', 'GET', '/api/admin/shop/inventory')
check('inventory list omits plaintext and ciphertext', 'synthetic-import-secret' not in json.dumps(inventory) and 'ciphertext' not in json.dumps(inventory))
product, _, _ = call(None, 'GET', '/api/shop/products/' + C['manual_product_id'])
order, _, _ = call('other_buyer', 'POST', '/api/shop/orders', {'product_id': product['id'], 'product_version': product['version'], 'terms_version': product['terms_version'], 'accepted_terms': True, 'buyer_input': {}}, str(uuid.uuid4()))
oid = order['order_no']
path = '/api/shop/orders/' + oid
payment, _, _ = call('other_buyer', 'POST', path + '/payment-attempts', {'expected_version': order['version'], 'payment_method': 'alipay'}, str(uuid.uuid4()))
checkout = urllib.parse.urlsplit(payment['checkout']['url'])
if (checkout.scheme, checkout.hostname, checkout.port) != (gateway.scheme, gateway.hostname, gateway.port) or checkout.username is not None or checkout.password is not None or (checkout.path != '/submit.php') or checkout.fragment:
    raise RuntimeError('payment checkout did not target the local simulator')
sim = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=ssl._create_unverified_context()), NoRedirect())
try:
    paid = sim.open(urllib.request.Request(payment['checkout']['url'], data=b'', method='POST'), timeout=10)
except urllib.error.HTTPError as e:
    paid = e
assert paid.status == 303
order, _, _ = call('other_buyer', 'GET', path)
call('other_buyer', 'POST', path + '/reconcile', {'expected_version': order['version']}, str(uuid.uuid4()))
deadline = time.monotonic() + 50
while time.monotonic() < deadline:
    order, _, _ = call('other_buyer', 'GET', path)
    if order['payment_state'] == 'paid':
        break
    time.sleep(1)
check('manual purchase is paid but requires operator delivery', order['payment_state'] == 'paid' and order['delivery_state'] != 'delivered')
apath = '/api/admin/shop/orders/' + oid
admin_order, _, _ = call('admin', 'GET', apath)
reason = 'Synthetic manual handover'
key = str(uuid.uuid4())
body = {'expected_version': admin_order['version'], 'reason': reason, 'secret_fields': [{'key': 'account', 'value': 'manual-http@example.invalid'}, {'key': 'password', 'value': 'manual-http-secret'}], 'instructions': 'Synthetic local use only'}
_, error, _ = call('admin', 'POST', apath + '/deliveries', body, key, expect=(403,))
check('manual delivery requires actual operation proof', not error['success'])
token = proof('shop.delivery.create', oid, admin_order['version'], key, 'first', {'reason': reason, 'template_version': admin_order['product_snapshot']['template_version']})
admin_order, _, _ = call('admin', 'POST', apath + '/deliveries', body, key, token)
check('manual delivery commits with bound password proof', admin_order['delivery_state'] == 'delivered')
replayed, _, _ = call('admin', 'POST', apath + '/deliveries', body, key)
check('manual delivery retry returns committed delivery without duplicate', replayed['current_delivery']['id'] == admin_order['current_delivery']['id'])
delivered, _, _ = call('other_buyer', 'POST', path + '/reveal', {'delivery_id': admin_order['current_delivery']['id'], 'intent': 'view'})
check('manual recipient can reveal exactly delivered fields', any((f['value'] == 'manual-http-secret' for f in delivered['fields'])))
(P / 'admin-acceptance-result.json').write_text(json.dumps({'order_no': oid, 'checks': results}, indent=2))
os.chmod(P / 'admin-acceptance-result.json', 0o600)
print('COMPLETE:', len(results), 'admin actual HTTP checks', flush=True)
