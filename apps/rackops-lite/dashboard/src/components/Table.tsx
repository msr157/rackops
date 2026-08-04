import { useMemo, useState } from 'react';
import { ChevronDown, ChevronUp } from 'lucide-react';

// DataTable: the comparison surface. Rows the operator scans and ranks get a
// real table — columns, sortable headers, numeric cells right-aligned in
// mono. Anything narrative (attention cards, feeds, wizards) stays a card.
//
// Sorting is client-side and stable: equal keys keep the caller's order, so
// a live-updating fleet doesn't shuffle under the cursor.

export interface Column<T> {
  key: string;
  label: React.ReactNode;
  /** Provide to make the column sortable. */
  sortValue?: (row: T) => string | number;
  /** Right-aligned mono numeric column. */
  num?: boolean;
  width?: number | string;
  render: (row: T) => React.ReactNode;
}

export function DataTable<T>({ columns, rows, rowKey, onRowClick, defaultSort, caption }: {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  /** Makes rows clickable. Keyboard users navigate via the real link the
      caller renders in the row's name cell. */
  onRowClick?: (row: T) => void;
  defaultSort?: { key: string; dir: 'asc' | 'desc' };
  caption: string;
}) {
  const [sort, setSort] = useState<{ key: string; dir: 'asc' | 'desc' } | null>(defaultSort ?? null);

  const sorted = useMemo(() => {
    if (!sort) return rows;
    const col = columns.find((c) => c.key === sort.key);
    if (!col?.sortValue) return rows;
    const dir = sort.dir === 'asc' ? 1 : -1;
    return rows
      .map((row, i) => ({ row, i }))
      .sort((a, b) => {
        const va = col.sortValue!(a.row);
        const vb = col.sortValue!(b.row);
        const cmp = typeof va === 'number' && typeof vb === 'number'
          ? va - vb
          : String(va).localeCompare(String(vb));
        return cmp !== 0 ? cmp * dir : a.i - b.i;
      })
      .map((x) => x.row);
  }, [rows, sort, columns]);

  const toggle = (key: string) => {
    setSort((s) => (s?.key === key
      ? { key, dir: s.dir === 'asc' ? 'desc' : 'asc' }
      : { key, dir: 'asc' }));
  };

  return (
    <div className="table-wrap">
      <table className="table">
        <caption className="sr-only">{caption}</caption>
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c.key} className={c.num ? 'num' : ''}
                style={c.width ? { width: c.width } : undefined}
                aria-sort={sort?.key === c.key ? (sort.dir === 'asc' ? 'ascending' : 'descending') : undefined}>
                {c.sortValue ? (
                  <button type="button" className="th-sort" onClick={() => toggle(c.key)}>
                    {c.label}
                    {sort?.key === c.key
                      ? (sort.dir === 'asc' ? <ChevronUp size={12} aria-hidden /> : <ChevronDown size={12} aria-hidden />)
                      : null}
                  </button>
                ) : c.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {sorted.map((row) => (
            <tr key={rowKey(row)} className={onRowClick ? 'clickable' : ''}
              onClick={onRowClick ? () => onRowClick(row) : undefined}>
              {columns.map((c) => (
                <td key={c.key} className={c.num ? 'num' : ''}>{c.render(row)}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
