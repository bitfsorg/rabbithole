interface Column<T> {
  key: string;
  label: string;
  mono?: boolean;
  render?: (row: T) => React.ReactNode;
}

interface DataTableProps<T> {
  columns: Column<T>[];
  data: T[];
  emptyMessage?: string;
}

export function DataTable<T extends Record<string, unknown>>({
  columns,
  data,
  emptyMessage = "No data",
}: DataTableProps<T>) {
  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border bg-bg-card">
            {columns.map((col) => (
              <th
                key={col.key}
                className="px-4 py-2 text-left text-xs font-medium text-text-secondary"
              >
                {col.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.length === 0 ? (
            <tr>
              <td
                colSpan={columns.length}
                className="px-4 py-8 text-center text-text-muted"
              >
                {emptyMessage}
              </td>
            </tr>
          ) : (
            data.map((row, i) => (
              <tr
                key={i}
                className="border-b border-border last:border-0 hover:bg-bg-card-hover"
              >
                {columns.map((col) => (
                  <td
                    key={col.key}
                    className={`px-4 py-2 ${col.mono ? "font-mono text-xs" : ""}`}
                  >
                    {col.render ? col.render(row) : String(row[col.key] ?? "")}
                  </td>
                ))}
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}
