#!/usr/bin/env python3
"""Actual HTTP automatic delivery, security proofs, refund and PostgreSQL accounting acceptance.

Start TestShopLiveHTTPServer first; no application API is mocked.
The sole positional argument is its private fixture directory.
This script creates synthetic business records and never accepts production URLs.
"""
import argparse
import hashlib
import http.cookiejar
import ipaddress
import json
import os
import pathlib
import re
import socket
import ssl
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


parser = argparse.ArgumentParser(description='Exercise the real shop HTTP fixture with synthetic local data only.')
parser.add_argument('fixture_dir', type=pathlib.Path, help='Private directory created for TestShopLiveHTTPServer; must contain credentials.json.')
parser.add_argument('--payment-events-only', action='store_true', help='Only verify callback review and confirmed-payment event uniqueness.')
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


if args.payment_events_only:
    runtime = json.loads((P / 'runtime.json').read_text())
    env = dict(line.split('=', 1) for line in (P / 'postgres.env').read_text().splitlines())
    assert re.fullmatch('kkai_test_shop_http_[a-zA-Z0-9_]+', env['POSTGRES_DB'])
    assert re.fullmatch('kk-shop-http-[a-f0-9]+', runtime['container'])

    def payment_facts(order_no):
        assert re.fullmatch('so_[a-f0-9]+', order_no)
        sql = """BEGIN READ ONLY;
SELECT count(*) FROM shop_payment_receipts WHERE order_id=(SELECT id FROM shop_orders WHERE public_id='%s');
SELECT count(*) FROM shop_payment_receipts WHERE order_id=(SELECT id FROM shop_orders WHERE public_id='%s') AND processing_state='review';
SELECT count(*) FROM shop_payment_receipts WHERE order_id=(SELECT id FROM shop_orders WHERE public_id='%s') AND processing_state='matched';
SELECT count(*) FROM shop_events WHERE order_id=(SELECT id FROM shop_orders WHERE public_id='%s') AND kind='payment_received' AND visibility='public';
SELECT count(DISTINCT receipt_id) FROM shop_events WHERE order_id=(SELECT id FROM shop_orders WHERE public_id='%s') AND kind='payment_received' AND visibility='public';
SELECT count(*) FROM shop_deliveries WHERE order_id=(SELECT id FROM shop_orders WHERE public_id='%s');
COMMIT;""" % ((order_no,) * 6)
        result = subprocess.run([
            'docker', '--context', 'desktop-linux', 'exec', '-i', runtime['container'],
            'psql', '-U', env['POSTGRES_USER'], '-d', env['POSTGRES_DB'],
            '-Atq', '-v', 'ON_ERROR_STOP=1',
        ], input=sql, text=True, capture_output=True, timeout=10)
        assert result.returncode == 0, 'local read-only payment facts query failed'
        return [int(value) for value in result.stdout.splitlines()]

    clients['buyer'] = urllib.request.build_opener(
        urllib.request.ProxyHandler({}), NoRedirect(),
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
    )
    login, _, _ = call('buyer', 'POST', '/api/user/login', {key: C['buyer'][key] for key in ['username', 'password']})
    tokens['buyer'] = login['access_token']
    product, _, _ = call(None, 'GET', '/api/shop/products/' + C['automatic_product_id'])
    order, _, _ = call('buyer', 'POST', '/api/shop/orders', {
        'product_id': product['id'], 'product_version': product['version'],
        'terms_version': product['terms_version'], 'accepted_terms': True, 'buyer_input': {},
    }, str(uuid.uuid4()))
    path = '/api/shop/orders/' + order['order_no']
    payment, _, _ = call('buyer', 'POST', path + '/payment-attempts', {
        'expected_version': order['version'], 'payment_method': 'alipay',
    }, str(uuid.uuid4()))
    checkout = urllib.parse.urlsplit(payment['checkout']['url'])
    if ((checkout.scheme, checkout.hostname, checkout.port) != (gateway.scheme, gateway.hostname, gateway.port)
            or checkout.username is not None or checkout.password is not None
            or checkout.path != '/submit.php' or checkout.fragment):
        raise RuntimeError('payment checkout did not target the local simulator')
    simulator = urllib.request.build_opener(
        urllib.request.ProxyHandler({}), NoRedirect(),
        urllib.request.HTTPSHandler(context=ssl._create_unverified_context()),
    )
    try:
        response = simulator.open(urllib.request.Request(payment['checkout']['url'], data=b'', method='POST'), timeout=10)
    except urllib.error.HTTPError as error:
        response = error
    check('signed local callback reaches actual shop HTTP route', response.status == 303)
    deadline = time.monotonic() + 10
    while True:
        review = payment_facts(order['order_no'])
        if review[1] == 1 or time.monotonic() >= deadline:
            break
        time.sleep(0.2)
    check('review retains one receipt with zero public payment events and zero deliveries', review == [1, 1, 0, 0, 0, 0])
    order, _, _ = call('buyer', 'GET', path)
    check('review HTTP timeline does not announce payment', all(event['event_type'] != 'payment_received' for event in order['timeline']))
    call('buyer', 'POST', path + '/reconcile', {'expected_version': order['version']}, str(uuid.uuid4()))
    deadline = time.monotonic() + 50
    while True:
        order, _, _ = call('buyer', 'GET', path)
        if order['delivery_state'] == 'delivered' or time.monotonic() >= deadline:
            break
        time.sleep(0.5)
    check('actual maintenance and outbox confirm and deliver', order['payment_state'] == 'paid' and order['delivery_state'] == 'delivered')
    final = payment_facts(order['order_no'])
    check('confirmed payment has one receipt one event and one delivery', final == [1, 0, 1, 1, 1, 1])
    check('final HTTP timeline announces payment exactly once', sum(event['event_type'] == 'payment_received' for event in order['timeline']) == 1)
    result_path = P / 'payment-event-result.json'
    result_path.write_text(json.dumps({'checks': results, 'review_counts': review, 'final_counts': final}, indent=2))
    result_path.chmod(0o600)
    print('COMPLETE:', len(results), 'focused real HTTP payment-event checks', flush=True)
    raise SystemExit(0)


for who in ['buyer', 'admin', 'other_buyer']:
    clients[who] = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect(), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    d, _, _ = call(who, 'POST', '/api/user/login', {k: C[who][k] for k in ['username', 'password']})
    tokens[who] = d['access_token']
check('real password login for three isolated users', len(tokens) == 3)
d, _, _ = call('buyer', 'POST', '/api/user/auth/refresh', {})
tokens['buyer'] = d['access_token']
check('real session cookie refresh', bool(tokens['buyer']))
product, _, _ = call(None, 'GET', '/api/shop/products/' + C['automatic_product_id'])
body = {'product_id': product['id'], 'product_version': product['version'], 'terms_version': product['terms_version'], 'accepted_terms': True, 'buyer_input': {}}
key = str(uuid.uuid4())
order, _, _ = call('buyer', 'POST', '/api/shop/orders', body, key)
oid = order['order_no']
path = '/api/shop/orders/' + oid
same, _, _ = call('buyer', 'POST', '/api/shop/orders', body, key)
check('same idempotency key creates exactly one order', same['order_no'] == oid)
recovered, _, _ = call('buyer', 'GET', '/api/shop/order-requests/' + key)
check('created order recovers by original request key', recovered['status'] == 'recorded' and recovered['order_no'] == oid)
_, error, _ = call('other_buyer', 'GET', path, expect=(404,))
check('another buyer cannot read order', error['code'] == 'SHOP_NOT_FOUND')
_, error, _ = call(None, 'GET', path, expect=(401,))
check('anonymous order read requires actual auth', not error['success'])
payment, _, _ = call('buyer', 'POST', path + '/payment-attempts', {'expected_version': order['version'], 'payment_method': 'alipay'}, str(uuid.uuid4()))
check('real payment adapter creates local checkout', payment['attempt']['state'] == 'ready' and urllib.parse.urlsplit(payment['checkout']['url']).netloc == gateway.netloc)
checkout = urllib.parse.urlsplit(payment['checkout']['url'])
if (checkout.scheme, checkout.hostname, checkout.port) != (gateway.scheme, gateway.hostname, gateway.port) or checkout.username is not None or checkout.password is not None or (checkout.path != '/submit.php') or checkout.fragment:
    raise RuntimeError('payment checkout did not target the local simulator')
sim = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=ssl._create_unverified_context()), NoRedirect())
try:
    paid = sim.open(urllib.request.Request(payment['checkout']['url'], data=b'', method='POST'), timeout=10)
except urllib.error.HTTPError as e:
    paid = e
check('local merchant signed callback accepted', paid.status == 303)
order, _, _ = call('buyer', 'GET', path)
_, _, _ = call('buyer', 'POST', path + '/reconcile', {'expected_version': order['version']}, str(uuid.uuid4()))
deadline = time.monotonic() + 50
while time.monotonic() < deadline:
    order, _, _ = call('buyer', 'GET', path)
    if order['delivery_state'] == 'delivered':
        break
    time.sleep(1)
check('real maintenance and outbox settle and deliver', order['payment_state'] == 'paid' and order['delivery_state'] == 'delivered')
check('ordinary detail exposes masked delivery only', 'fictional-secret-' not in json.dumps(order))
delivery = order['current_delivery']['id']
reveal = {'delivery_id': delivery, 'intent': 'view'}
secret, _, headers = call('buyer', 'POST', path + '/reveal', reveal)
check('owner can reveal synthetic stock and response is no-store', headers['Cache-Control'] == 'no-store' and len(secret['fields']) == 2)
_, error, _ = call('other_buyer', 'POST', path + '/reveal', reveal, expect=(404,))
check('another buyer cannot reveal delivered secret', error['code'] == 'SHOP_NOT_FOUND')

def proof(scope, target, version, key, action, details):
    context = {'action': action, 'target_id': target, 'expected_version': version, 'idempotency_key': key, 'details': details}
    data, _, _ = call('admin', 'POST', '/api/verify', {'method': 'password', 'password': C['admin']['password'], 'scope': scope, 'context': context})
    return data['proof_token']
receipts, _, _ = call('admin', 'GET', '/api/admin/shop/receipts?order_no=' + oid)
check('one unique receipt from signed callback and provider query', len(receipts['items']) == 1)
rid = receipts['items'][0]['id']
rpath = '/api/admin/shop/receipts/' + rid
receipt, _, _ = call('admin', 'GET', rpath)
money = {k: receipt['money'][k] for k in ['amount_minor', 'currency', 'exponent']}

def evidence(status, ref=''):
    receipt, _, _ = call('admin', 'GET', rpath)
    key = str(uuid.uuid4())
    observed = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
    reference = 'local-simulator/' + key
    summary = 'Synthetic merchant evidence for local acceptance'
    body = {'receipt_id': rid, 'receipt_version': receipt['version'], 'source': 'merchant_console', 'provider_transaction_id': receipt['provider_transaction_id'], 'money': money, 'observed_status': status, 'observed_at': observed, 'reference': reference, 'summary': summary}
    details = {'source': 'merchant_console', 'provider_transaction_id': receipt['provider_transaction_id'], 'money': money, 'observed_status': status, 'observed_at': observed, 'reference_digest': hashlib.sha256(reference.encode()).hexdigest(), 'summary_digest': hashlib.sha256(summary.encode()).hexdigest()}
    if ref:
        body['provider_refund_or_adjustment_id'] = ref
        details['provider_refund_or_adjustment_id'] = ref
    token = proof('shop.payment.evidence.create', rid, receipt['version'], key, 'create', details)
    data, _, _ = call('admin', 'POST', '/api/admin/shop/evidence', body, key, token)
    return data['evidence_id']
preflight = evidence('not_refunded')
receipt, _, _ = call('admin', 'GET', rpath)
key = str(uuid.uuid4())
reason = 'Synthetic local refund acceptance'
body = {'expected_version': receipt['version'], 'kind': 'goods', 'reason': reason, 'evidence_refs': [preflight]}
_, error, _ = call('admin', 'POST', rpath + '/refunds', body, key, expect=(403,))
check('refund approval requires actual security proof', not error['success'])
token = proof('shop.refund.manage', rid, receipt['version'], key, 'approve', {'kind': 'goods', 'money': money, 'reason': reason, 'evidence_ids': [preflight]})
refund, _, _ = call('admin', 'POST', rpath + '/refunds', body, key, token)
check('scope-bound password proof approves refund', refund['state'] == 'approved')
_, error, _ = call('buyer', 'POST', path + '/reveal', reveal, expect=(403,))
check('refund approval immediately blocks secret reveal', error['code'] == 'SHOP_REVEAL_BLOCKED')
fpath = '/api/admin/shop/refunds/' + refund['id']
key = str(uuid.uuid4())
reason = 'Synthetic manual original-channel operation'
token = proof('shop.refund.manage', refund['id'], refund['version'], key, 'register_attempt', {'reason': reason, 'evidence_ids': [preflight]})
refund, _, _ = call('admin', 'POST', fpath + '/attempts', {'expected_version': refund['version'], 'reason': reason, 'preflight_evidence_refs': [preflight]}, key, token)
attempt = refund['attempts'][-1]['id']
ref = 'local-refund-' + str(uuid.uuid4())
operation_time = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
paid_evidence = evidence('refund_succeeded', ref)
refund, _, _ = call('admin', 'GET', fpath)
key = str(uuid.uuid4())
token = proof('shop.refund.manage', refund['id'], refund['version'], key, 'record_attempt', {'attempt_id': attempt, 'operated_at': operation_time, 'evidence_ids': [paid_evidence], 'channel_ref': ref, 'public_note': 'Synthetic local refund recorded'})
refund, _, _ = call('admin', 'POST', fpath + '/attempts/' + attempt + '/record', {'expected_version': refund['version'], 'operated_at': operation_time, 'evidence_refs': [paid_evidence], 'channel_ref': ref, 'public_note': 'Synthetic local refund recorded'}, key, token)
check('recorded refund remains unknown until verification', refund['state'] == 'unknown')
key = str(uuid.uuid4())
token = proof('shop.refund.manage', refund['id'], refund['version'], key, 'verify_attempt', {'attempt_id': attempt, 'conclusion': 'succeeded', 'evidence_ids': [paid_evidence]})
refund, _, _ = call('admin', 'POST', fpath + '/attempts/' + attempt + '/verify', {'expected_version': refund['version'], 'conclusion': 'succeeded', 'evidence_refs': [paid_evidence]}, key, token)
check('evidenced refund completes through actual HTTP lifecycle', refund['state'] == 'succeeded')
order, _, _ = call('buyer', 'GET', path)
check('buyer order reflects refunded state', order['refund_state'] == 'succeeded')
user, _, _ = call('buyer', 'GET', '/api/user/self')
check('shop payment and refund preserve API quota', user['quota'] == 12345)
runtime = json.loads((P / 'runtime.json').read_text())
env = dict((line.split('=', 1) for line in (P / 'postgres.env').read_text().splitlines()))
assert re.fullmatch('kkai_test_shop_http_[a-zA-Z0-9_]+', env['POSTGRES_DB']), 'only the disposable fixture database is accepted'
assert re.fullmatch('kk-shop-http-[a-f0-9]+', runtime['container']), 'only the disposable local container is accepted'
assert re.fullmatch('sr_[a-f0-9]+', rid), 'invalid receipt reference'
sql = "SELECT count(*),COALESCE(sum(amount_minor),0) FROM shop_payment_adjustments WHERE state='succeeded' AND receipt_id=(SELECT id FROM shop_payment_receipts WHERE public_id='" + rid + "'); SELECT count(*) FROM top_ups WHERE user_id=1001;"
r = subprocess.run(['docker', '--context', 'desktop-linux', 'exec', '-i', runtime['container'], 'psql', '-U', env['POSTGRES_USER'], '-d', env['POSTGRES_DB'], '-At', '-v', 'ON_ERROR_STOP=1'], input=sql, text=True, capture_output=True, timeout=10)
assert r.returncode == 0
check('one refund debit and no wallet top-up in PostgreSQL', r.stdout.strip().splitlines() == ['1|125', '0'])
(P / 'acceptance-result.json').write_text(json.dumps({'order_no': oid, 'receipt_id': rid, 'refund_id': refund['id'], 'checks': results}, indent=2))
os.chmod(P / 'acceptance-result.json', 0o600)
print('COMPLETE:', len(results), 'real HTTP checks', flush=True)
