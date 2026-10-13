"""Minimal ANSI screen replay, adapted from Maestro's PTY smoke harness."""
import re
import unicodedata

def screen(raw, width=80, height=24):
    grid = [[' ']*width for _ in range(height)]
    row, col = 0, 0
    top, bottom = 0, height-1
    def scroll_region(n, down=False):
        n = min(n, bottom-top+1)
        region = grid[top:bottom+1]
        empty = [[' ']*width for _ in range(n)]
        grid[top:bottom+1] = empty+region[:-n] if down else region[n:]+empty
    def line_feed():
        nonlocal row
        if row == bottom: scroll_region(1)
        else: row = min(height-1, row+1)
    text = raw.decode('utf8', errors='ignore')
    i = 0
    while i < len(text):
        if text[i] == '\x1b':
            match = re.match(r'\x1b\[([0-9;?:><]*)([ -/]*)([@-~])', text[i:])
            if match:
                params, _, command = match.groups()
                nums = [int(x) if x else 0 for x in params.lstrip('?').split(';') if x.isdigit() or x == '']
                n = nums[0] if nums and nums[0] else 1
                if command in 'Hf':
                    row = max(0, min(height-1, n-1)); col = max(0, min(width-1, (nums[1] if len(nums)>1 and nums[1] else 1)-1))
                elif command == 'A': row = max(0, row-n)
                elif command == 'B': row = min(height-1, row+n)
                elif command == 'C': col = min(width-1, col+n)
                elif command == 'D': col = max(0, col-n)
                elif command == 'E': row = min(height-1,row+n); col = 0
                elif command == 'F': row = max(0,row-n); col = 0
                elif command == 'd': row = max(0, min(height-1, n-1))
                elif command == 'G': col = max(0, min(width-1, n-1))
                elif command == 'r':
                    top = max(0,min(height-1,n-1))
                    bottom = max(top,min(height-1,(nums[1] if len(nums)>1 and nums[1] else height)-1))
                    row, col = 0, 0
                elif command == 'S': scroll_region(n)
                elif command == 'T': scroll_region(n,down=True)
                elif command == 'X':
                    for j in range(col,min(width,col+n)): grid[row][j] = ' '
                elif command == 'P':
                    count = min(n,width-col)
                    grid[row][col:] = grid[row][col+count:] + [' ']*count
                elif command == 'b':
                    ch = grid[row][max(0,col-1)]
                    for j in range(col,min(width,col+n)): grid[row][j] = ch
                    col = min(width,col+n)
                elif command == 'J' and nums and nums[0] in [2,3]: grid = [[' ']*width for _ in range(height)]
                elif command == 'K':
                    mode = nums[0] if nums else 0
                    start, end = (0,width) if mode == 2 else (0,col+1) if mode == 1 else (col,width)
                    for j in range(start,end): grid[row][j] = ' '
                i += len(match.group()); continue
            osc = re.match(r'\x1b\].*?(?:\x07|\x1b\\)', text[i:], flags=re.S)
            if osc: i += len(osc.group()); continue
            if text[i:i+2] == '\x1bM':
                if row == top: scroll_region(1,down=True)
                else: row = max(0,row-1)
            i += 2; continue
        ch = text[i]; i += 1
        if ch == '\r': col = 0
        elif ch == '\n': line_feed()
        elif ch == '\b': col = max(0,col-1)
        elif ch == '\t': col = min(width-1,(col//8+1)*8)
        elif not unicodedata.category(ch).startswith('C'):
            if col >= width: col = 0; line_feed()
            grid[row][col] = ch
            col += 2 if unicodedata.east_asian_width(ch) in ['W','F'] else 1
    return '\n'.join(''.join(line) for line in grid)
