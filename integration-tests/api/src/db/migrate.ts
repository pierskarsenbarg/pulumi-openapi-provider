import { Database } from "bun:sqlite";
import { drizzle } from "drizzle-orm/bun-sqlite";
import { migrate } from "drizzle-orm/bun-sqlite/migrator";

const dbUrl = process.env.DATABASE_URL ?? "dev.db";
const sqlite = new Database(dbUrl);
sqlite.exec("PRAGMA foreign_keys = ON;");

const db = drizzle(sqlite);
migrate(db, { migrationsFolder: "./migrations" });

console.log("Migrations applied to", dbUrl);
sqlite.close();
