<template>
  <GradientBackground class="menu-root">
    <el-header class="menu-header">
      <h1>AI应用平台</h1>
      <el-button type="danger" @click="handleLogout">退出登录</el-button>
    </el-header>
    <el-main class="menu-main">
      <div class="menu-grid">
        <el-card
          v-for="item in menuItems"
          :key="item.path"
          class="menu-item"
          @click="$router.push(item.path)"
        >
          <div class="card-content">
            <el-icon size="48" :color="item.color">
              <component :is="item.icon" />
            </el-icon>
            <h3>{{ item.title }}</h3>
            <p>{{ item.desc }}</p>
          </div>
        </el-card>
      </div>
    </el-main>
  </GradientBackground>
</template>

<script>
import {useRouter} from "vue-router";
import {ElMessage, ElMessageBox} from "element-plus";
import {ChatDotRound, Camera} from "@element-plus/icons-vue";
import GradientBackground from "../components/GradientBackground.vue";

export default {
  name: "MenuView",
  components: {GradientBackground, ChatDotRound, Camera},
  setup() {
    const router = useRouter();

    // 菜单项配置化：新增入口只需在此追加一项，模板通过 v-for 渲染
    const menuItems = [
      {
        path: "/ai-chat",
        title: "AI聊天",
        desc: "与AI进行智能对话",
        icon: "ChatDotRound",
        color: "#409eff",
      },
      {
        path: "/image-recognition",
        title: "图像识别",
        desc: "上传图片进行AI识别",
        icon: "Camera",
        color: "#67c23a",
      },
    ];

    const handleLogout = async () => {
      try {
        await ElMessageBox.confirm("确定要退出登录吗？", "提示", {
          confirmButtonText: "确定",
          cancelButtonText: "取消",
          type: "warning",
        });
        localStorage.removeItem("token");
        ElMessage.success("退出登录成功");
        router.push("/login");
      } catch {
        // 用户取消操作
      }
    };

    return {menuItems, handleLogout};
  },
};
</script>

<style scoped>
.menu-header {
  background: rgba(255, 255, 255, 0.1);
  backdrop-filter: blur(10px);
  color: white;
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0 30px;
  box-shadow: 0 2px 20px rgba(0, 0, 0, 0.1);
  border-bottom: 1px solid rgba(255, 255, 255, 0.1);
  position: relative;
  z-index: 2;
}

.menu-header h1 {
  margin: 0;
  font-size: 28px;
  font-weight: 600;
  background: linear-gradient(
    135deg,
    #ffffff 0%,
    rgba(255, 255, 255, 0.8) 100%
  );
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

.menu-header :deep(.el-button) {
  background: rgba(255, 255, 255, 0.2);
  border: 1px solid rgba(255, 255, 255, 0.3);
  color: white;
  transition: all 0.3s var(--ease-out);
}

.menu-header :deep(.el-button:hover) {
  background: rgba(255, 255, 255, 0.3);
  transform: translateY(-2px);
  box-shadow: 0 8px 25px rgba(0, 0, 0, 0.2);
}

.menu-main {
  flex: 1;
  display: flex;
  justify-content: center;
  align-items: center;
  position: relative;
  z-index: 1;
}

.menu-root :deep(.gb-content) {
  display: flex;
  flex-direction: column;
  flex: 1;
  width: 100%;
  min-height: 100vh;
}

.menu-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 40px;
  max-width: 900px;
  width: 100%;
  padding: 40px;
  animation: menu-fade-in 1s var(--ease-out);
}

@keyframes menu-fade-in {
  from {
    opacity: 0;
    transform: translateY(50px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.menu-item {
  cursor: pointer;
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(15px);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-md);
  border: 1px solid rgba(255, 255, 255, 0.2);
  transition: all 0.4s cubic-bezier(0.175, 0.885, 0.32, 1.275);
  position: relative;
  overflow: hidden;
  animation: menu-card-in 0.8s var(--ease-out) both;
}

.menu-item:nth-child(1) {
  animation-delay: 0.1s;
}
.menu-item:nth-child(2) {
  animation-delay: 0.2s;
}

@keyframes menu-card-in {
  from {
    opacity: 0;
    transform: translateY(60px) rotateX(10deg);
  }
  to {
    opacity: 1;
    transform: translateY(0) rotateX(0deg);
  }
}

.menu-item::before {
  content: "";
  position: absolute;
  top: 0;
  left: -100%;
  width: 100%;
  height: 100%;
  background: linear-gradient(
    90deg,
    transparent,
    rgba(255, 255, 255, 0.4),
    transparent
  );
  transition: left 0.6s;
}

.menu-item:hover::before {
  left: 100%;
}

.menu-item:hover {
  transform: translateY(-15px) scale(1.05);
  box-shadow: var(--shadow-lg);
}

.card-content {
  text-align: center;
  padding: 50px 30px;
  position: relative;
  z-index: 1;
}

.card-content :deep(.el-icon) {
  display: block;
  margin: 0 auto 20px;
  transition: all 0.3s var(--ease-out);
}

.menu-item:hover .card-content :deep(.el-icon) {
  transform: scale(1.2) rotate(5deg);
}

.card-content h3 {
  margin: 0 0 15px 0;
  color: var(--color-text);
  font-size: 24px;
  font-weight: 600;
  transition: all 0.3s var(--ease-out);
}

.menu-item:hover h3 {
  color: #409eff;
  transform: translateY(-5px);
}

.card-content p {
  margin: 0;
  color: var(--color-text-secondary);
  font-size: 16px;
  line-height: 1.6;
  transition: all 0.3s var(--ease-out);
}

.menu-item:hover p {
  color: #34495e;
  transform: translateY(-3px);
}
</style>
