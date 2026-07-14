import { useAuth } from "./context/AuthContext";
import { LoginScreen } from "./screens/LoginScreen";
import { OwnerApp } from "./OwnerApp";

export default function App() {
  const { auth } = useAuth();
  return auth ? <OwnerApp /> : <LoginScreen />;
}
