#!/usr/bin/env python3
"""Isolated localhost-only fixture broadcaster and end-to-end audit assertions.

No private keys: uses Anvil's unlocked development accounts. Production Go
packages only read RPC. Requires forge, cast, anvil and Python 3.9+.
"""
import argparse
import csv
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
COLUMNS = ['entry_id', 'account_id', 'chain_id', 'token_address', 'deposit_address',
           'expected_amount_base_units', 'tx_hash', 'log_index', 'credit_status',
           'credited_at_block', 'recorded_block_hash']


def run():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-dir', default=str(ROOT / 'demo'))
    parser.add_argument('--integration', action='store_true')
    args = parser.parse_args()
    dest = Path(args.output_dir).resolve()
    dest.mkdir(parents=True, exist_ok=True)
    binary = os.environ.get('LEDGERCHAIN_BIN', str(ROOT / 'bin/ledgerchain'))
    subprocess.run(['forge', 'build'], cwd=ROOT, check=True, timeout=120)
    artifact = json.loads((ROOT / 'out/TestToken.sol/TestToken.json').read_text())
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    endpoint = f'http://127.0.0.1:{port}'
    with tempfile.TemporaryFile(mode='w+') as log:
        process = subprocess.Popen(['anvil', '--host', '127.0.0.1', '--port', str(port),
                                    '--chain-id', '31337', '--silent'], stdout=log, stderr=log)
        try:
            def rpc(method, params):
                req = urllib.request.Request(endpoint, json.dumps({
                    'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params,
                }).encode(), {'Content-Type': 'application/json'})
                with urllib.request.urlopen(req, timeout=5) as response:
                    result = json.load(response)
                if 'error' in result:
                    raise RuntimeError(f'Local fixture RPC {method}: {result["error"]}')
                return result['result']

            deadline = time.monotonic() + 15
            while True:
                try:
                    chain_id = rpc('eth_chainId', [])
                    break
                except (OSError, ValueError):
                    if process.poll() is not None or time.monotonic() > deadline:
                        raise RuntimeError('Anvil failed to start')
                    time.sleep(0.05)
            assert int(chain_id, 16) == 31337, 'refusing fixture writes on another chain'
            accounts = rpc('eth_accounts', [])
            sender, alice, bob = accounts[:3]

            def send(data, to=None):
                tx = {'from': sender, 'data': data, 'gas': hex(5000000)}
                if to:
                    tx['to'] = to
                tx_hash = rpc('eth_sendTransaction', [tx])
                deadline = time.monotonic() + 10
                while True:
                    receipt = rpc('eth_getTransactionReceipt', [tx_hash])
                    if receipt:
                        return receipt
                    if time.monotonic() > deadline:
                        raise RuntimeError('fixture receipt timeout')
                    time.sleep(0.02)

            def calldata(sig, *values):
                return subprocess.check_output(['cast', 'calldata', sig, *map(str, values)], text=True, timeout=10).strip()

            # Constructor takes one uint8, encoded as a full ABI word.
            deployed = send(artifact['bytecode']['object'] + f'{6:064x}')
            assert int(deployed['status'], 16) == 1
            token = deployed['contractAddress'].lower()
            send(calldata('mint(address,uint256)', sender, 10**30), token)
            snapshot = rpc('evm_snapshot', [])
            transfers = send(calldata('multiTransfer(address[],uint256[])',
                                      f'[{alice},{bob},{alice}]', '[1000000,2000000,3000000]'), token)
            assert len(transfers['logs']) == 3
            rpc('anvil_mine', ['0x3'])
            head = int(rpc('eth_blockNumber', []), 16)
            with (dest / 'accounts.csv').open('w', newline='') as f:
                w = csv.writer(f)
                w.writerow(['account_id', 'deposit_address'])
                w.writerows([['alice', alice], ['bob', bob]])
            (dest / 'config.yaml').write_text(f'''network:
  chain_id: "31337"
  rpc_url: "{endpoint}"
asset:
  token_address: "{token}"
  symbol: TESTUSD
  decimals: 6
policy:
  min_confirmations: 3
  include_mint_transfers: false
discovery:
  from_block: 0
  to_block: latest
  chunk_size: 2
  max_blocks: 1000
rpc:
  timeout_seconds: 5
  run_timeout_seconds: 60
  max_concurrency: 4
  max_retries: 1
output:
  format: json
''')
            rows = []
            for i, event in enumerate(transfers['logs']):
                rows.append([f'credit_{i}', ['alice', 'bob', 'alice'][i], '31337', token,
                             [alice, bob, alice][i], str([1000000, 2000000, 3000000][i]),
                             event['transactionHash'], str(int(event['logIndex'], 16)),
                             'credited', str(head), event['blockHash']])

            def ledger(name, records):
                with (dest / name).open('w', newline='') as f:
                    w = csv.writer(f)
                    w.writerow(COLUMNS)
                    w.writerows(records)
            ledger('ledger.csv', rows)
            bad = [list(rows[0]), list(rows[0]), list(rows[1])]
            bad[0][5] = '900000'
            bad[1][0] = 'duplicate_credit'
            bad[2][1] = 'alice'
            bad[2][4] = alice
            ledger('inconsistent.csv', bad)

            # Same logical source, exact TEXT amounts, independently read-only.
            import sqlite3
            source = dest / 'ledger.sqlite'
            if source.exists():
                source.unlink()
            with sqlite3.connect(source) as db:
                db.execute('CREATE TABLE ledger_entries (' + ','.join(c + ' TEXT' for c in COLUMNS) + ')')
                db.executemany('INSERT INTO ledger_entries VALUES (' + ','.join('?' for _ in COLUMNS) + ')', rows)

            def audit(name, ledger_name='ledger.csv', state=False):
                cmd = [binary, 'reconcile', '--config', str(dest / 'config.yaml'),
                       '--accounts', str(dest / 'accounts.csv'), '--ledger', str(dest / ledger_name),
                       '--output', str(dest / name)]
                if ledger_name.endswith('.sqlite'):
                    cmd += ['--ledger-type', 'sqlite']
                if state:
                    cmd += ['--state-db', str(dest / 'history.sqlite')]
                result = subprocess.run(cmd, cwd=ROOT, timeout=90)
                if not (dest / name).exists():
                    raise RuntimeError(f'audit failed with exit {result.returncode}')
                return result.returncode, json.loads((dest / name).read_text())

            # Fresh scope on every demo; don't mix previous ephemeral Anvil histories.
            for suffix in ['', '-wal', '-shm']:
                p = dest / ('history.sqlite' + suffix)
                if p.exists():
                    p.unlink()
            code, clean = audit('clean-report.json', state=True)
            assert code == 0 and clean['summary']['verified_entries'] == 3, clean
            code, repeated = audit('repeat-report.json', state=True)
            assert code == 0 and repeated['findings'] == clean['findings']
            code, sqlite_report = audit('sqlite-report.json', 'ledger.sqlite')
            assert code == 0 and sqlite_report['summary']['verified_entries'] == 3
            code, bad_report = audit('discrepancy-report.json', 'inconsistent.csv')
            codes = {f['code'] for f in bad_report['findings']}
            assert code == 1 and {'DUPLICATE_CREDIT', 'AMOUNT_MISMATCH',
                                  'RECIPIENT_MISMATCH', 'ACCOUNT_ATTRIBUTION_MISMATCH',
                                  'UNRECORDED_ONCHAIN_TRANSFER'} <= codes, bad_report
            if args.integration:
                assert rpc('evm_revert', [snapshot]) is True
                # Produce a different block at the stored event height.
                rpc('evm_setNextBlockTimestamp', [int(rpc('eth_getBlockByNumber', ['latest', False])['timestamp'], 16) + 100])
                rpc('anvil_mine', ['0x4'])
                code, reorg = audit('reorg-report.json', state=True)
                assert code == 3, reorg
                assert any(f['code'] == 'REORG_OBSERVED' for f in reorg['findings']), reorg
                assert reorg['summary']['verified_entries'] == 0
            print(f'Demo assertions passed. Reports and source fixtures: {dest}')
            print('The temporary localhost Anvil process is stopped on exit. Re-run make demo to regenerate live fixtures.')
        finally:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()


if __name__ == '__main__':
    run()
