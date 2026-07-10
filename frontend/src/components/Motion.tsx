import { motion, useReducedMotion } from "framer-motion";

// A restrained content transition: a short fade + slight rise. Used on tab and
// view switches to give the shell a calm, considered feel (Raycast-adjacent),
// and it self-disables under prefers-reduced-motion.
export function FadeIn({ id, children }: { id: string | number; children: React.ReactNode }) {
  const reduce = useReducedMotion();
  return (
    <motion.div
      key={id}
      initial={reduce ? false : { opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.18, ease: "easeOut" }}
      className="h-full"
    >
      {children}
    </motion.div>
  );
}
