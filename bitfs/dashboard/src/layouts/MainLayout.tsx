import { NavLink, Outlet } from "react-router-dom";
import {
  LayoutDashboard,
  HardDrive,
  Globe,
  Wallet,
  ScrollText,
} from "lucide-react";

const navItems = [
  { to: "/", label: "Dashboard", icon: LayoutDashboard },
  { to: "/storage", label: "Storage", icon: HardDrive },
  { to: "/network", label: "Network", icon: Globe },
  { to: "/wallet", label: "Wallet", icon: Wallet },
  { to: "/logs", label: "Logs", icon: ScrollText },
];

function MainLayout() {
  return (
    <div className="flex h-screen bg-bg-primary text-text-primary">
      <aside className="flex w-56 flex-col border-r border-border bg-bg-sidebar">
        <div className="flex h-14 items-center border-b border-border px-4">
          <span className="text-lg font-semibold text-accent">BitFS</span>
        </div>
        <nav className="flex-1 space-y-1 p-2">
          {navItems.map(({ to, label, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              end={to === "/"}
              className={({ isActive }) =>
                `flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                  isActive
                    ? "bg-bg-card text-accent"
                    : "text-text-secondary hover:bg-bg-card hover:text-text-primary"
                }`
              }
            >
              <Icon className="h-4 w-4" />
              {label}
            </NavLink>
          ))}
        </nav>
        <div className="border-t border-border p-3 text-xs text-text-muted">
          BitFS LFCP Dashboard
        </div>
      </aside>
      <main className="flex-1 overflow-auto p-6">
        <Outlet />
      </main>
    </div>
  );
}

export default MainLayout;
