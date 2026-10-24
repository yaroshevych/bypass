"""Real tmux journeys rendered to PNG. Review artifacts, not pixel baselines."""
import argparse
import hashlib
import html
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import tempfile
import time

import PIL
from fixture import build, MASTER
from render import render, ANSI

PROJECT = Path(__file__).resolve().parents[2]
SCREENS = ('welcome', 'unlock', 'workspace', 'details', 'tabs', 'tabs-hover', 'palette', 'shortcuts',
           'editor', 'generator', 'delete', 'grand-password', 'grand-notes',
           'attachments', 'history', 'file-picker', 'pin')


def capture(args, fixture, cli, output):
    name = f'{args.screen}-{args.appearance}-{args.theme}-{args.width}x{args.height}'
    with tempfile.TemporaryDirectory(prefix='bypass-visual-') as directory:
        root = Path(directory)
        env = os.environ.copy()
        env.update(TMUX='', TERM='xterm-256color', COLORTERM='truecolor',
                   CLICOLOR_FORCE='1', NO_COLOR='', TZ='UTC', LC_ALL='C.UTF-8')
        for key in ('HOME', 'XDG_CONFIG_HOME', 'XDG_STATE_HOME', 'XDG_CACHE_HOME', 'XDG_DATA_HOME'):
            path = root / key.lower()
            path.mkdir(mode=0o700)
            env[key] = str(path)
        config = Path(env['XDG_CONFIG_HOME']) / 'bypass'
        config.mkdir(mode=0o700)
        (config / 'config.json').write_text(json.dumps(dict(appearance=args.appearance, theme=args.theme)))
        vault = root / 'demo-vault.kdbx'
        shutil.copyfile(fixture, vault)
        # Never let a visual journey access the desktop clipboard.
        tools = root / 'tools'
        tools.mkdir()
        for command in ('pbcopy', 'pbpaste', 'wl-copy', 'wl-paste', 'xclip'):
            tool = tools / command
            tool.write_text('#!/bin/sh\nexit 0\n')
            tool.chmod(0o700)
        env['PATH'] = str(tools) + os.pathsep + env['PATH']
        launcher = root / 'launch.sh'
        keys = ('HOME', 'XDG_CONFIG_HOME', 'XDG_STATE_HOME', 'XDG_CACHE_HOME', 'XDG_DATA_HOME',
                'TERM', 'COLORTERM', 'CLICOLOR_FORCE', 'NO_COLOR', 'TZ', 'LC_ALL', 'PATH')
        launcher.write_text('#!/bin/sh\nexec env ' + ' '.join(k + '=' + shlex.quote(env[k]) for k in keys)
                            + ' ' + shlex.join([str(PROJECT / 'bin/bypass'), '--backend', str(cli)]) + '\n')
        launcher.chmod(0o700)
        tmux_config = root / 'tmux.conf'
        tmux_config.write_text('set -g status off\nset -g history-limit 0\n')
        socket = f'bypass-visual-{os.getpid()}'

        def tmux(*commands):
            result = subprocess.run(['tmux', '-L', socket, *map(str, commands)], env=env,
                                    text=True, capture_output=True, timeout=10)
            if result.returncode:
                raise RuntimeError(result.stderr.strip())
            return result.stdout

        def screen():
            return tmux('capture-pane', '-e', '-p', '-t', 'visual')

        def wait(marker):
            deadline, previous, stable = time.monotonic() + 15, None, 0
            while time.monotonic() < deadline:
                value = screen()
                stable = stable + 1 if value == previous else 0
                if marker.casefold() in ANSI.sub('', value).casefold() and stable >= 3:
                    return value
                previous = value
                time.sleep(.04)
            (output / (name + '.failed.txt')).write_text(ANSI.sub('', screen()))
            raise RuntimeError(f'{name}: did not reach {marker!r}')

        def key(*values):
            tmux('send-keys', '-t', 'visual', *values)

        def text(value):
            key('-l', value)

        def palette(query, marker):
            key('C-p')
            wait('Commands')
            text(query)
            wait(query)
            key('Enter')
            wait(marker)

        try:
            tmux('-f', tmux_config, 'new-session', '-d', '-x', 120, '-y', 35,
                 '-s', 'visual', '-c', PROJECT, str(launcher))
            wait('Open Vault')
            if args.screen != 'welcome':
                key('C-o')
                wait('Path ·')
                if args.screen != 'file-picker':
                    key('C-u')
                    text(str(vault))
                    key('Enter')
                    wait('Unlock Vault')
                    if args.screen != 'unlock':
                        text(MASTER)
                        key('Enter')
                        wait('Proton Mail')
                        key('Right')
                        wait('/ filter')
                        if args.screen == 'details':
                            key('Right')
                            wait('c copy')
                        elif args.screen in ('tabs', 'tabs-hover'):
                            key('t')
                            if args.screen == 'tabs-hover':
                                value = wait('2 Proton Mail')
                                row = 1 if args.appearance == 'compact' else 2
                                column = ANSI.sub('', value).splitlines()[row].index('+') + 1
                                text(f'\x1b[<35;{column};{row+1}M')
                        elif args.screen == 'palette':
                            key('C-p')
                            wait('Commands')
                        elif args.screen == 'shortcuts':
                            key('?')
                            wait('Keyboard')
                        elif args.screen == 'editor':
                            key('e')
                            wait('Ctrl+S')
                        elif args.screen == 'generator':
                            key('C-g')
                            wait('Generator')
                        elif args.screen == 'delete':
                            key('d')
                            wait('Move to Recycle Bin')
                        elif args.screen.startswith('grand-'):
                            key('Right')
                            # Username, password, URL, notes.
                            key(*(['Down'] * (1 if args.screen == 'grand-password' else 3)))
                            key('G')
                            wait('Grand View')
                        elif args.screen == 'attachments':
                            palette('Attachments', 'Attachments')
                        elif args.screen == 'history':
                            palette('History', 'History')
                        elif args.screen == 'pin':
                            key('C-k')
                            wait('Set Session PIN')
                            text('2468')
                            key('Enter')
                            wait('Confirm Session PIN')
                            text('2468')
                            key('Enter')
                            wait('Screen protected')
            tmux('resize-window', '-t', 'visual', '-x', args.width, '-y', args.height)
            value = wait('')
            (output / (name + '.ansi')).write_text(value)
            (output / (name + '.txt')).write_text(ANSI.sub('', value))
            fg, bg = ((76, 79, 105), (239, 241, 245)) if args.theme == 'latte' else ((202, 211, 245), (36, 39, 58))
            render(value, output / (name + '.png'), args.width, args.height, args.font, 20, fg, bg)
            metadata = vars(args).copy()
            metadata.update(font_sha256=hashlib.sha256(Path(args.font).read_bytes()).hexdigest(),
                            pillow=PIL.__version__, capture='tmux SGR rasterization',
                            fixture='synthetic native KeePassXC vault', baseline=False)
            (output / (name + '.json')).write_text(json.dumps(metadata, indent=2))
            print(output / (name + '.png'), flush=True)
        finally:
            subprocess.run(['tmux', '-L', socket, 'kill-server'], env=env, capture_output=True, timeout=10)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('screen', nargs='?', choices=SCREENS, default='workspace')
    parser.add_argument('--appearance', choices=('classic', 'compact'), default='classic')
    parser.add_argument('--theme', choices=('macchiato', 'latte'), default='macchiato')
    parser.add_argument('--width', type=int, default=120)
    parser.add_argument('--height', type=int, default=35)
    parser.add_argument('--font', default=os.environ.get('BYPASS_SCREENSHOT_FONT', '/System/Library/Fonts/Menlo.ttc'))
    parser.add_argument('--output', default=str(PROJECT / 'evals/artifacts/screenshots'))
    parser.add_argument('--all', action='store_true')
    parser.add_argument('--list', action='store_true')
    args = parser.parse_args()
    if args.list:
        print('\n'.join(SCREENS))
        return
    if args.width < 20 or args.height < 8:
        parser.error('minimum capture size is 20x8')
    output = Path(args.output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='bypass-visual-fixture-') as directory:
        fixture, cli = build(Path(directory))
        if args.all:
            matrix = [(s, 'classic', 'macchiato', 120, 35) for s in SCREENS]
            matrix += [(s, 'compact', 'macchiato', 120, 35) for s in ('workspace', 'tabs', 'palette')]
            matrix += [(s, 'compact', 'latte', 120, 35) for s in ('workspace', 'palette')]
            matrix += [('workspace', 'compact', 'macchiato', 90, 25),
                       ('workspace', 'compact', 'macchiato', 60, 20),
                       ('editor', 'compact', 'macchiato', 60, 20),
                       ('unlock', 'compact', 'macchiato', 48, 14)]
            for s, appearance, theme, width, height in matrix:
                capture(argparse.Namespace(**(vars(args) | dict(screen=s, appearance=appearance,
                        theme=theme, width=width, height=height))), fixture, cli, output)
        else:
            capture(args, fixture, cli, output)
    images = sorted(output.glob('*.png'))
    gallery = '<!doctype html><meta charset="utf-8"><title>Bypass visual review</title><style>body{background:#181926;color:#cad3f5;font:16px monospace}img{max-width:100%;display:block}figure{margin:24px 0}</style><h1>Visual review — synthetic data</h1><p>tmux captures rasterized with a fixed font. No approved pixel baselines yet.</p>'
    for path in images:
        gallery += f'<figure><figcaption>{html.escape(path.stem)}</figcaption><a href="{path.name}"><img loading="lazy" src="{path.name}"></a></figure>'
    (output / 'index.html').write_text(gallery)
    print(output / 'index.html')


if __name__ == '__main__':
    main()
