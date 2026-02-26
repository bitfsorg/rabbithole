import { BrowserRouter, Routes, Route } from "react-router-dom";
import MainLayout from "./layouts/MainLayout";
import DashboardHome from "./pages/DashboardHome";
import Storage from "./pages/Storage";
import Network from "./pages/Network";
import Wallet from "./pages/Wallet";
import Logs from "./pages/Logs";

function App() {
  return (
    <BrowserRouter basename="/_dashboard">
      <Routes>
        <Route element={<MainLayout />}>
          <Route index element={<DashboardHome />} />
          <Route path="storage" element={<Storage />} />
          <Route path="network" element={<Network />} />
          <Route path="wallet" element={<Wallet />} />
          <Route path="logs" element={<Logs />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}

export default App;
