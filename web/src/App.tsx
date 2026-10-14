import { useQuery, useQueryClient } from "@tanstack/react-query";
import { NavLink, Navigate, Outlet, Route, Routes, useLocation } from "react-router";
import { ApiError, api, call } from "./api/client";
import { Login } from "./pages/Login";
import { Services } from "./pages/Services";

function useMe() {
  return useQuery({
    queryKey: ["me"],
    queryFn: () => call(api.getMe()),
  });
}

function Shell() {
  const me = useMe();
  const queryClient = useQueryClient();
  const location = useLocation();

  if (me.isPending) return null;
  if (me.error instanceof ApiError && me.error.status === 401) {
    return <Navigate to="/login" state={{ from: location.pathname }} replace />;
  }
  if (me.isError) return <p className="error">{me.error.message}</p>;

  const logout = async () => {
    await call(api.logout());
    queryClient.clear();
    window.location.assign("/app/login");
  };

  return (
    <div className="app">
      <aside className="side">
        <span className="brand">modularnost</span>
        <NavLink to="/" end className="item">
          Services
        </NavLink>
        <div className="who">
          {me.data.username} · {me.data.role}
          <button className="link" onClick={logout}>
            Sign out
          </button>
        </div>
      </aside>
      <main className="main">
        <Outlet />
      </main>
    </div>
  );
}

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route element={<Shell />}>
        <Route index element={<Services />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
